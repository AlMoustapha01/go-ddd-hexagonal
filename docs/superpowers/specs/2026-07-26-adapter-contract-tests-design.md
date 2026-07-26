# Tests de contrat partagés entre les adaptateurs memory et postgres

**Date :** 2026-07-26
**Portée :** `internal/adapters/`
**Nature :** ajout de tests + corrections d'écarts de comportement révélés par ces tests

## Problème

Les adaptateurs Postgres ne sont jamais exécutés :

```
internal/adapters/outbound/postgres    coverage: 0.0% of statements
```

Environ 700 lignes portant tout le SQL du projet, qu'aucun test ne parcourt. `go build`
ne valide pas une requête : un nom de colonne erroné, un `Scan` dans le mauvais ordre
ou une transaction non propagée ne se découvrent qu'en production.

Plus important pour ce repo : la promesse centrale de l'architecture hexagonale —
`memory` et `postgres` interchangeables derrière le même port — **n'est vérifiée nulle
part**. Les tests `memory` passent, ceux de `postgres` n'existent pas, donc rien ne
garantit que les deux implémentations honorent le même contrat.

## Solution

Une suite de tests écrite **une fois contre le port**, exécutée contre les deux
adaptateurs.

### Architecture

Un package **non-test** `internal/adapters/contract`, importable par les deux packages
de test d'adaptateurs. C'est le patron de la bibliothèque standard (`testing/fstest`,
`testing/iotest`) : du code qui importe `testing` mais que seuls des tests importent,
donc absent du binaire de production.

```
internal/adapters/contract/
  contract.go              doc du package + helpers partagés
  user_repository.go       func UserRepository(t, newRepo)
  order_repository.go
  order_read_store.go
  account_event_store.go
  outbox_store.go
```

Chaque fonction prend une **fabrique** plutôt qu'une instance, pour que chaque
sous-test reçoive un magasin vierge :

```go
func UserRepository(t *testing.T, newRepo func(*testing.T) user.Repository) {
	t.Run("FindByID returns ErrNotFound for an unknown id", func(t *testing.T) {
		repo := newRepo(t)   // magasin neuf, isolé
		...
	})
}
```

Branchement côté adaptateurs :

```go
// memory/contract_test.go
func TestUserRepositoryContract(t *testing.T) {
	contract.UserRepository(t, func(*testing.T) domainuser.Repository {
		return memory.NewUserRepository()
	})
}

// postgres/contract_test.go
func TestUserRepositoryContract(t *testing.T) {
	contract.UserRepository(t, func(t *testing.T) domainuser.Repository {
		return postgres.NewUserRepository(freshDB(t))
	})
}
```

### Périmètre

| Port | Contrat partagé | Contenu |
|---|---|---|
| `user.Repository` | oui | round-trip Save/Find, `ErrNotFound` sur les trois chemins, `ExistsByEmail`, pagination et ordre (récent d'abord), Delete puis re-Delete → `ErrNotFound` |
| `order.Repository` | oui | round-trip avec lignes et `Money` préservés, `ListByCustomer`, pagination, Delete |
| `apporder.ReadStore` | oui | Upsert écrase, Get → `ErrNotFound`, List ordonnée, `ListByCustomer`, Delete d'un inconnu est un no-op |
| `account.EventStore` | oui | append + replay fidèle, concurrence optimiste, stream inconnu → `ErrNotFound` |
| `shared.OutboxStore` | oui | Add puis Fetch (plus ancien d'abord), respect de `limit`, MarkPublished retire du lot suivant |
| `shared.Atomic` | **non — volontairement** | voir ci-dessous |

`shared.Atomic` ne peut pas avoir de contrat partagé : `memory.Transactor` est un
no-op explicitement documenté (« There is no real transaction, so writes are NOT
atomic together »). Lui imposer le contrat de Postgres le ferait échouer par
conception.

Il reçoit à la place un **test spécifique à Postgres** qui est la preuve de
correction du pattern Outbox : dans un `Within` qui échoue, ni la ligne `orders`
ni la ligne `outbox` ne subsistent ; dans un `Within` qui réussit, les deux sont
présentes. C'est aussi ce test qui attraperait la régression si
`order_repository.Delete` (qui attaque `r.pool` directement, ligne 85) était un
jour appelé à l'intérieur d'un `Within`.

### Provisioning de Postgres — sans nouvelle dépendance

`freshDB(t)` :

1. Lit `TEST_DATABASE_URL` ; si elle est absente, `t.Skip` avec un message explicite.
2. Applique `migrations/*.up.sql` dans l'ordre, en lisant les fichiers et en les
   exécutant. Tous les `CREATE` sont déjà en `IF NOT EXISTS`, donc l'opération est
   idempotente et compatible avec une base déjà migrée par `make docker-up`.
   Aucune dépendance sur golang-migrate en bibliothèque.
3. Avant chaque sous-test :
   `TRUNCATE users, orders, order_read, outbox, account_events RESTART IDENTITY CASCADE`.

Les étapes 1 et 2 s'exécutent une seule fois par binaire de test (`sync.Once`).

`go.mod` reste **inchangé** : trois dépendances directes.

### Pas de balise de build

Les tests Postgres reposent uniquement sur `t.Skip`, pas sur `-tags=integration`.
Un fichier derrière une balise de build est invisible au compilateur et au linter
dans le job normal : il peut cesser de compiler sans que la CI ne s'en aperçoive,
ce qui est précisément le mode de panne qu'on cherche à éliminer. Sans balise,
`go vet` et `golangci-lint` couvrent le code d'intégration en permanence, et
`go test ./...` reste vert sur un poste sans base de données.

## Écarts de comportement révélés

### Corrigés : slice `nil` au lieu de slice vide

Une méthode de liste qui ne trouve rien doit renvoyer `[]`, pas `nil` — sinon la
même API sérialise `null` avec un adaptateur et `[]` avec l'autre. Le contrat
l'exige ; cinq sites sont corrigés :

| Fichier | Méthode |
|---|---|
| `memory/order_repository.go` | `ListByCustomer` |
| `postgres/user_repository.go` | `List` |
| `postgres/order_repository.go` | `queryOrders` (sert `List` et `ListByCustomer`) |
| `postgres/order_read_store.go` | `querySummaries` (sert `List` et `ListByCustomer`) |
| `postgres/outbox_store.go` | `FetchUnpublished` |

### Corrigé : 500 au lieu de 404 sur un identifiant malformé (non anticipé)

Découvert en exécutant le contrat contre une vraie base. Les ports du côté
lecture prennent des `string`, et `order_handler.go` passe `chi.URLParam(r, "id")`
jusqu'à `ReadStore.Get` **sans aucune validation**. Les colonnes `order_read.id`
et `order_read.customer_id` sont typées `UUID`, donc :

```
GET /api/v1/orders/pas-un-uuid
  driver memory   -> ErrNotFound         -> 404
  driver postgres -> SQLSTATE 22P02      -> 500
```

Bug utilisateur réel, atteignable depuis l'API publique, présent uniquement avec
le driver Postgres. Le port promet `ErrNotFound` pour ce qui n'est pas trouvé, et
une clé qui ne peut pas être un UUID ne peut rien désigner : `Get`,
`ListByCustomer` et `Delete` du read store Postgres traitent désormais une clé
malformée comme « pas de ligne » au lieu de remonter une erreur SQL. Le contrat
verrouille ce comportement (`a malformed id behaves as no such row`).

`Upsert` reste strict : y écrire sous une clé malformée serait une erreur de
programmation, pas une requête utilisateur.

### Corrigé : ordre de l'outbox en mémoire (non anticipé)

`memory.OutboxStore.FetchUnpublished` renvoyait les enregistrements dans leur
ordre d'insertion, alors que le port promet « oldest first » et que Postgres
applique `ORDER BY occurred_at`. Les deux coïncident en production, mais la
promesse porte sur les événements et non sur l'ordre d'écriture. L'adaptateur
mémoire trie désormais sur `OccurredAt` (tri stable, pour que des horodatages
égaux conservent leur ordre d'insertion).

### Documentés sans être assertés

Un contrat n'a le droit d'exiger que ce que le port promet. Ces trois écarts sont
signalés en commentaire dans les fichiers de contrat, mais pas transformés en
échec de test — les corriger serait un changement de comportement, pas un ajout
de test, et c'est une décision distincte.

1. **Unicité de l'email.** Postgres porte une contrainte `UNIQUE` sur `users.email` ;
   `memory` n'en a aucune et accepte deux utilisateurs avec le même email. Le port
   `user.Repository` ne promet nulle part l'unicité — c'est la couche application
   qui la garde via `ExistsByEmail`. Resserrer le port impliquerait de faire
   remonter `ErrEmailTaken` depuis Postgres et d'ajouter un index en mémoire.

2. **`FetchUnpublished` avec `limit <= 0`.** `memory` renvoie tout, Postgres renvoie
   zéro ligne (`LIMIT 0`). Inatteignable en production : le relais passe toujours
   `batchSize: 100` (`relay.go:49`). Le contrat n'exerce que `limit > 0`.

3. **`EventStore.Append` avec `expectedVersion` supérieur à la version réelle.**
   `memory` renvoie `ErrConcurrencyConflict` ; Postgres insère dans le trou de
   numérotation et réussit. Postgres viole ici la lettre du port (« ONLY IF the
   current stored version equals expectedVersion »), mais le cas est inatteignable
   avec un appelant correct, qui passe toujours la version qu'il vient de charger.
   Le contrat exerce le cas réel — l'écrivain périmé, `expectedVersion` inférieur
   à la version réelle — que les deux adaptateurs traitent identiquement.

### Comparaison du payload d'outbox

La colonne `outbox.payload` est de type `JSONB`, qui normalise les espaces et
l'ordre des clés. Le contrat compare donc les payloads **en tant que documents
JSON**, pas octet par octet : ce que le port promet, c'est que le document
survive, pas sa mise en forme.

## Outillage

**Makefile** — deux nouvelles cibles : `test-db` (démarre le seul service
Postgres de compose) et `test-integration` (exporte `TEST_DATABASE_URL` et lance
la suite complète). `TEST_DATABASE_URL` retombe par défaut sur `DATABASE_URL`.

**CI** — le job `build-test` gagne un service `postgres:16-alpine` avec son
healthcheck et la variable `TEST_DATABASE_URL`, de sorte que les tests de contrat
Postgres s'exécutent à chaque push au lieu d'être ignorés.

**Lint** — `.golangci.yml` étend aux fichiers de `adapters/contract/` les
relaxations déjà accordées aux `_test.go`. C'est du code de test qui ne peut pas
vivre dans des fichiers `_test.go` puisque les deux packages de test d'adaptateurs
doivent l'importer ; de longues tables de sous-tests `t.Run` déclenchent `gocyclo`
par construction.

## Vérification

Résultats mesurés, base PostgreSQL 16 réelle via Docker :

- `go test -race -count=1 ./...` **sans** base : tout est vert, les contrats
  Postgres s'ignorent avec le message
  « TEST_DATABASE_URL is not set — skipping the Postgres contract ».
- `go test -race -count=1 ./...` **avec** base : 8 packages au vert, les deux
  séries de contrats exécutées pour de bon.
- Couverture :

  | Package | Avant | Après |
  |---|---|---|
  | `adapters/outbound/postgres` | 0,0 % | **81,5 %** |
  | `adapters/outbound/memory` | 10,0 % | **96,5 %** |

- `go vet`, `gofmt -s`, `golangci-lint run ./...` : 0 issue.

## Hors périmètre

Pas de tests sur les handlers HTTP (déjà à 61 %), pas de couverture de la couche
application (chantier distinct), pas de bancs de performance.
