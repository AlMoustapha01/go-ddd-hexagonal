# go-ddd-hex

Boilerplate Go appliquant le **Domain-Driven Design (DDD)** et l'**architecture hexagonale** (ports & adapters / Clean Architecture). Prêt à cloner, à renommer et à étendre.

Deux bounded contexts d'exemple :

- **User / Account** — register, activate, deactivate, rename, list, delete.
- **Order** — agrégat riche (Money, OrderLine, machine à états draft → placed → paid / cancelled) qui **référence le client par son identité**, illustre une **réaction inter-agrégats** via un bus d'événements (désactiver un utilisateur annule ses commandes ouvertes), met en œuvre **CQRS** (écriture et lecture séparées, projections maintenues par un projecteur) et le **pattern Outbox** (événements écrits transactionnellement avec l'état, relayés au moins une fois — fini le *dual-write*).
- **Account** — agrégat **event-sourcé** (un wallet : open, deposit, withdraw). Son état n'est **jamais stocké** : seule la suite d'événements l'est, et le solde est reconstruit en **rejouant** le flux. Illustre le replay, la **concurrence optimiste** par version, et l'**audit trail** natif (historique complet des événements).

## Principe : la règle de dépendance

Les dépendances pointent **toujours vers l'intérieur**. Le domaine ne connaît rien du monde extérieur ; l'extérieur dépend du domaine via des interfaces (**ports**).

```
                 ┌───────────────────────────────────────────────┐
                 │              Adapters (infrastructure)         │
                 │                                                │
   HTTP (chi) ──▶│  inbound/http ──▶ [PORT: UseCases]             │
   CLI / gRPC ──▶│                          │                     │
                 │                          ▼                     │
                 │                   Application (use cases)       │
                 │                          │                     │
                 │                          ▼                     │
                 │                     Domain (cœur métier)        │
                 │                          ▲                     │
                 │        [PORT: Repository] │ [PORT: EventPublisher]
                 │                          │                     │
                 │  outbound/postgres ──────┤                     │
                 │  outbound/memory  ───────┘                     │
                 └───────────────────────────────────────────────┘
```

- **Domain** (`internal/domain`) : entités, agrégats, value objects, événements, erreurs métier et **définition des ports** (interfaces). Aucune dépendance externe (uniquement `google/uuid` pour l'identité).
- **Application** (`internal/application`) : les **use cases**. Orchestrent le domaine via les ports. Aucune règle métier, aucune infra.
- **Adapters** (`internal/adapters`) :
  - *inbound* (driving) : `http` traduit HTTP ↔ port entrant `UseCases`.
  - *outbound* (driven) : `postgres` et `memory` implémentent le port `Repository`.
- **Platform** (`internal/platform`) : config, logging — détails techniques transverses.
- **Bootstrap** (`internal/bootstrap`) : la **composition root**, seul endroit qui câble les adaptateurs concrets.

## Arborescence

```
cmd/api/                     Point d'entrée (main, graceful shutdown)
internal/
  domain/
    shared/                  Shared kernel : Event/EventPublisher, Atomic (UoW),
                             OutboxStore, Keyed, BaseEvent
    user/                    Agrégat User, Email (VO), events, port Repository
    order/                   Agrégat Order, Money/Line/Status (VO), CustomerID, events, port
    account/                 Event sourcing : agrégat rejoué, events, codec, port EventStore
  application/
    user/                    Use cases User (Service), commands, DTOs, port entrant
    order/                   CQRS : command side (service.go, écrit via outbox+UoW)
                             + query side (query_service.go, read_model.go, projector.go)
    account/                 Event sourcing : command (load→decide→append) + query (fold + history)
    integration/             Handlers inter-contextes (le SEUL package qui connaît les deux)
  adapters/
    inbound/http/            Adaptateur HTTP (chi) : handlers User + Order + Account, tests
    outbound/memory/         Write repos, read store, outbox store, transactor, event store
    outbound/postgres/       Idem en pgx (tx via ctx ; event store append-only)
    outbound/eventbus/       Bus d'événements in-process (implémente shared.EventPublisher)
    outbound/outbox/         Outbox : Publisher (écrit) + Relay (poll → bus, at-least-once)
  platform/config/           Chargement de configuration (env)
  platform/logging/          Logger structuré (slog)
  bootstrap/                 Composition root (wiring)
migrations/                  Migrations SQL (golang-migrate)
```

## Démarrage rapide

Prérequis : Go 1.25+ (imposé par prometheus/client_golang).

```bash
# 1. Récupérer les dépendances
make tidy         # (ou: go mod tidy)

# 2. Lancer les tests
make test

# 3. Démarrer l'API (repository in-memory, zéro dépendance externe)
make run
```

L'API écoute sur `:8080`.

### Tests de contrat des adaptateurs

Les adaptateurs `memory` et `postgres` sont tenus par une **suite de tests de
contrat unique** (`internal/adapters/contract`), écrite une fois contre chaque
port et rejouée contre les deux implémentations. C'est ce qui fait de
« les adaptateurs sont interchangeables » une propriété vérifiée plutôt qu'une
promesse : si les deux divergent, la même suite échoue sur l'un d'eux.

`make test` ignore la série Postgres faute de base de données. Pour la jouer
réellement :

```bash
make test-db            # démarre uniquement Postgres via docker compose
make test-integration   # rejoue toute la suite, contrats Postgres compris
```

La CI fournit un service Postgres, donc les deux séries s'exécutent à chaque push.

```bash
# Health
curl localhost:8080/health

# Créer un utilisateur
curl -X POST localhost:8080/api/v1/users \
  -H 'Content-Type: application/json' \
  -d '{"name":"Alice","email":"alice@example.com"}'

# Lister
curl localhost:8080/api/v1/users

# Activer (remplacer {id})
curl -X POST localhost:8080/api/v1/users/{id}/activate
```

## Avec PostgreSQL

Tout est fourni par `docker compose` (Postgres + migrations + API) :

```bash
make docker-up      # build + db + migrate + api
```

Ou en local, en pointant vers une base existante :

```bash
export DB_DRIVER=postgres
export DATABASE_URL='postgres://app:app@localhost:5432/app?sslmode=disable'
make migrate-up     # nécessite l'outil golang-migrate
make run
```

## Endpoints

| Méthode | Chemin                             | Description               |
|---------|------------------------------------|---------------------------|
| GET     | `/health`                          | **Liveness** — le process répond (200 même base injoignable) |
| GET     | `/ready`                           | **Readiness** — les dépendances répondent (**503** si la base ne répond pas) |
| GET     | `/metrics`                         | Exposition Prometheus |
| POST    | `/api/v1/users`                    | Créer un utilisateur      |
| GET     | `/api/v1/users?limit=&offset=`     | Lister                    |
| GET     | `/api/v1/users/{id}`               | Récupérer par ID          |
| PATCH   | `/api/v1/users/{id}`               | Renommer                  |
| POST    | `/api/v1/users/{id}/activate`      | Activer                   |
| POST    | `/api/v1/users/{id}/deactivate`    | Désactiver (→ annule ses commandes ouvertes) |
| DELETE  | `/api/v1/users/{id}`               | Supprimer                 |
| POST    | `/api/v1/orders`                   | **Commande** : créer (draft) — client actif requis |
| POST    | `/api/v1/orders/{id}/lines`        | **Commande** : ajouter une ligne |
| POST    | `/api/v1/orders/{id}/place`        | **Commande** : soumettre  |
| POST    | `/api/v1/orders/{id}/pay`          | **Commande** : payer      |
| POST    | `/api/v1/orders/{id}/cancel`       | **Commande** : annuler (raison optionnelle) |
| GET     | `/api/v1/orders?limit=&offset=`    | **Requête** : liste (résumés, read model) |
| GET     | `/api/v1/orders/{id}`              | **Requête** : détail (read model) |
| GET     | `/api/v1/customers/{customerID}/orders` | **Requête** : commandes d'un client (résumés) |

Les routes **Commande** (POST) passent par le côté écriture (agrégat + write store) ; les routes **Requête** (GET) sont servies par le read model CQRS.

### Account (event-sourcé)

| Méthode | Chemin                                | Description               |
|---------|---------------------------------------|---------------------------|
| POST    | `/api/v1/accounts`                    | Ouvrir un compte          |
| POST    | `/api/v1/accounts/{id}/deposit`       | Déposer (`amount_minor`)  |
| POST    | `/api/v1/accounts/{id}/withdraw`      | Retirer (400 si solde insuffisant) |
| GET     | `/api/v1/accounts/{id}`               | État courant (reconstruit par replay) |
| GET     | `/api/v1/accounts/{id}/history`       | Journal complet des événements (audit) |

## Relations entre agrégats & bus d'événements

Ce boilerplate illustre deux règles centrales du DDD sur la façon dont des agrégats se coordonnent **sans se coupler**.

**1. On référence les autres agrégats par identité, pas par pointeur.** Une commande ne contient pas un objet `User` ; elle stocke un `CustomerID` (value object). Conséquence concrète : le package `domain/order` **n'importe jamais** `domain/user`. Les deux contextes restent indépendants et peuvent évoluer séparément.

**2. La cohérence inter-agrégats se fait à terme, via des événements.** Quand une règle traverse deux agrégats, on ne l'exécute pas dans une transaction commune :

- La règle « on ne crée une commande que pour un client actif » est vérifiée dans le use case `order` à travers un port étroit `CustomerChecker`. L'adaptateur `integration.UserCustomerChecker` traduit cet appel vers le repository `user` — c'est une couche anti-corruption.
- La règle « désactiver un client annule ses commandes ouvertes » passe par le **bus d'événements** : `User.Deactivate()` enregistre un événement `user.deactivated`, le use case le publie après persistance, et un handler abonné (`integration.CancelOrdersOnUserDeactivation`) annule les commandes. L'agrégat `User` ignore totalement l'existence des commandes.

Le package `internal/application/integration` est le **seul** autorisé à connaître les deux contextes : en y isolant les handlers de pont, on garde `user` et `order` découplés. Le bus (`adapters/outbound/eventbus`) est synchrone et in-process ; le remplacer par Kafka/NATS ne touche ni le domaine ni l'application.

```
User.Deactivate()  ──record──▶  user.deactivated
        │ (persisté d'abord)
        ▼
   EventPublisher (bus)  ──dispatch──▶  CancelOrdersOnUserDeactivation
                                              │
                                              ▼
                                     order.Cancel("customer deactivated")
```

## CQRS (contexte Order)

Le contexte `order` sépare explicitement l'écriture de la lecture — c'est **CQRS** (Command Query Responsibility Segregation).

**Côté commande (écriture).** Les opérations qui changent l'état (`Create`, `AddLine`, `Place`, `Pay`, `Cancel`) passent par le service de commande, qui charge l'agrégat, applique la logique métier, persiste dans le **write store** (`order.Repository`) et publie des événements de domaine. Port entrant : `order.UseCases`.

**Côté requête (lecture).** Les lectures ne touchent jamais l'agrégat. Elles sont servies par un **read model** dénormalisé (`SummaryView` pour les listes, `DetailView` pour le détail) stocké dans un **store séparé** (`order.ReadStore`) — une table `order_read` distincte en Postgres, une map dédiée en mémoire. Port entrant : `order.Queries`.

**Le projecteur** (`order.Projector`) est le pont entre les deux. Il est abonné à tous les événements `order.*` sur le bus ; à chaque événement il recharge l'agrégat depuis le write store, le projette en read models et les upsert dans le read store.

```
Commande ─▶ Service (write) ─▶ write store (agrégat)
                │
                └─ publie event ─▶ bus ─▶ Projector ─▶ read store (projections)
                                                              ▲
Requête ─────────────────────────── QueryService ────────────┘
```

Avec le bus synchrone in-process fourni, la projection est mise à jour dans la même requête (**read-your-writes**). Avec un bus asynchrone (Kafka…), la lecture devient cohérente à terme — **le code des requêtes ne change pas**.

Choix assumé : le projecteur **recharge l'agrégat depuis le write store** à chaque événement, ce qui garde les événements légers et le projecteur simple. Le contexte `user` reste volontairement en CRUD simple — le même patron CQRS s'y applique à l'identique si besoin.

## Outbox pattern (le cours)

### Le problème : le *dual-write*

Une commande doit souvent faire **deux choses qui vont ensemble** : (1) enregistrer un changement d'état en base, et (2) publier un événement (vers un bus, un broker Kafka/RabbitMQ, un autre service). Ce sont **deux systèmes différents**, sans transaction commune. Regarde ce code naïf (c'est ce que fait encore le contexte `user` de ce repo, volontairement) :

```go
repo.Save(ctx, user)        // 1) commit en base
bus.Publish(ctx, event)     // 2) publication
```

Que se passe-t-il si le process **crashe entre (1) et (2)** ? La base dit « l'utilisateur est désactivé », mais l'événement `user.deactivated` n'est **jamais** parti. Toutes les réactions en aval (projection CQRS, annulation des commandes, e-mail, service de facturation…) ne se déclencheront jamais. À l'inverse, si tu publies **avant** de committer et que le commit échoue, tu as annoncé un fait qui n'a pas eu lieu. Il n'existe pas de transaction distribuée fiable et simple entre une base et un broker. C'est le **dual-write problem**, et il est insidieux : tout marche 99,9 % du temps, puis un crash au mauvais moment corrompt silencieusement la cohérence entre services.

### La solution : l'outbox

L'idée est de **ne plus faire deux écritures dans deux systèmes**, mais **une seule écriture transactionnelle** dans la base :

1. Dans **la même transaction** que le changement d'état, on **INSÈRE les événements** dans une table `outbox`. Comme c'est une seule transaction SQL, soit l'état **et** les événements sont persistés, soit **rien** ne l'est. L'atomicité est garantie par la base.
2. Un processus séparé, le **relay** (ou *message relay* / *dispatcher*), lit régulièrement les lignes non publiées de l'`outbox`, les publie sur le bus/broker, puis les marque comme publiées.

```
   ┌─────────────── une seule transaction DB ───────────────┐
   │  UPDATE orders SET ...                                  │
   │  INSERT INTO outbox (event, ...)                        │   ← atomique
   └────────────────────────────────────────────────────────┘
                              │  (plus tard)
                    Relay ────┴──▶ lit outbox ──▶ publie sur le bus ──▶ marque publié
```

### Propriété clé : *at-least-once* + idempotence

Le relay publie **puis** marque comme publié. S'il crashe **entre les deux**, la ligne reste « non publiée » et sera **republiée** au prochain passage. Donc un événement est délivré **au moins une fois** (`at-least-once`) — jamais perdu, mais **possiblement en double**. Corollaire indispensable : **les consommateurs doivent être idempotents** (traiter deux fois le même événement doit donner le même résultat). Ici le projecteur est naturellement idempotent : il recharge l'agrégat et fait un `UPSERT` — rejouer l'événement réécrit la même projection.

### Comment c'est implémenté ici (contexte `order`)

- **Unit of Work** (`shared.Atomic`, impl. `postgres.Transactor`) : `Within(ctx, fn)` ouvre une transaction, la place dans le `context`, et tout ce qui l'utilise y participe (voir `queryEngine`).
- Le service de commande écrit l'agrégat **et** les événements dans la même transaction :

  ```go
  s.uow.Within(ctx, func(ctx) error {
      if err := s.repo.Save(ctx, o); err != nil { return err }
      return s.outbox.Publish(ctx, o.PullEvents()...) // INSERT dans outbox, même tx
  })
  ```

- Le **relay** (`outbox.Relay`) tourne en tâche de fond (`Start`) et draine l'`outbox` vers le bus ; le projecteur, abonné, met à jour le read model.
- **Latence** : juste après le commit, le service déclenche aussi un drain **inline** (`Dispatcher`) pour publier immédiatement (faible latence + *read-your-writes*). Le poller de fond reste le **filet de sécurité** qui rattrape ce qu'un crash aurait laissé. Retirer le drain inline et ne garder que le poller ⇒ découplage total, cohérence à terme.
- En Postgres, `FetchUnpublished` utilise `FOR UPDATE SKIP LOCKED` pour permettre **plusieurs relays** en parallèle sans double traitement.

> Le driver **in-memory** n'a pas de vraie transaction (le `Transactor` est un no-op) : il exerce le flux complet mais **seul Postgres fournit la garantie d'atomicité**. Le contexte `user` reste en publication directe **exprès**, pour que tu voies côte à côte le code vulnérable (dual-write) et le code protégé (outbox) — le passer à l'outbox est identique au contexte `order`.

### Quand l'utiliser, et les alternatives

L'outbox est justifié dès que **la perte d'un événement casse la cohérence entre agrégats ou services** (paiements, stock, notifications, intégrations). Coût : une table, un relay à opérer, et un peu de latence (polling). Alternatives et compléments :

- **CDC / log-tailing** (ex. **Debezium** qui lit le WAL Postgres) : au lieu de *poller* la table outbox, un connecteur lit le journal de transactions et publie — moins de latence, pas de polling, mais plus d'infra.
- **Listen/Notify** (Postgres `NOTIFY`) pour réveiller le relay sans polling serré.
- **Event sourcing** : si l'agrégat EST son flux d'événements, le « store » et l'« outbox » fusionnent.
- **Ordre** : si l'ordre par agrégat compte, publier par `aggregate_id` (partition/clé) et trier par `occurred_at`.

## Event sourcing (le cours — contexte `account`)

### L'idée : l'état est **dérivé**, pas stocké

Dans un CRUD classique (et même dans l'agrégat `Order` de ce repo), on stocke **l'état courant** : une ligne `orders` avec `status = 'paid'`. On **écrase** l'ancienne valeur à chaque changement — l'historique est perdu.

L'**event sourcing** inverse la logique : on ne stocke **jamais l'état**, on stocke la **suite des événements** qui l'ont produit. L'état courant est **calculé** en rejouant ces événements :

```
état = fold(events)      // on part de zéro et on applique chaque événement dans l'ordre
```

Pour le compte `Account`, le flux `[Opened(EUR), Deposited(5000), Withdrawn(1200)]` **est** la donnée ; le solde `3800` n'est qu'une **projection** de ce flux, recalculée à la demande. Le store est **append-only** : on n'écrase ni ne supprime jamais une ligne, on ajoute.

### Comment c'est implémenté ici

- L'agrégat (`domain/account`) a une méthode `mutate(event)` — la **fonction de transition d'état** — utilisée **à l'identique** pour appliquer un nouvel événement (`raise`) et pour rejouer les événements persistés (`Rehydrate`). C'est ce qui garantit que « rejouer » et « exécuter » donnent le même résultat (testé par `TestReplayEqualsLive`).
- Une **commande** ne modifie pas une ligne : elle **charge le flux → reconstruit l'agrégat → décide → ajoute** de nouveaux événements. Voir `application/account/service.go` :

  ```go
  events, _ := store.Load(ctx, id)      // 1. charger le flux
  a := account.Rehydrate(events)        // 2. reconstruire l'état (fold)
  expected := a.Version()
  a.Withdraw(amount)                     // 3. décider (valide l'invariant de solde)
  store.Append(ctx, id, expected, a.PendingEvents())  // 4. ajouter les nouveaux événements
  ```

- Le **port `EventStore`** (`Load` / `Append`) remplace le `Repository`. Impl. in-memory + Postgres (table `account_events` append-only).
- Un **codec** sérialise/désérialise les événements (`Encode`/`Decode`). C'est vital : les événements stockés doivent être **reconstructibles des années plus tard**, donc le payload est une forme explicite et stable — jamais le struct Go brut. Testé par `TestCodecRoundTrip`.

### Concurrence optimiste par **version**

Deux requêtes qui modifient le même compte en parallèle pourraient se marcher dessus. La parade : chaque événement a un **numéro de version** séquentiel, et `Append` n'accepte que si la **version attendue** correspond à la version stockée. En Postgres c'est gratuit via la clé primaire `(aggregate_id, version)` : deux writers ne peuvent pas insérer la même version suivante, le perdant reçoit une violation d'unicité → `409 Conflict`, il recharge et rejoue. Testé par `TestAccountEventStoreOptimisticConcurrency`.

### Ce que ça apporte

- **Audit trail natif** : l'historique complet est la donnée (voir `GET /accounts/{id}/history`). Précieux en finance, compliance, debug.
- **Voyage dans le temps** : rejouer jusqu'à l'événement *n* donne l'état à cet instant.
- **Nouvelles projections rétroactives** : besoin d'une nouvelle vue ? On la construit en rejoutant tout l'historique.
- **Découplage naturel avec CQRS** : les événements alimentent des read models (comme le projecteur d'`Order`).

### Le coût, et quand l'utiliser

Ce n'est **pas** gratuit : les requêtes complexes exigent des **projections** (le fold sur des flux longs est coûteux → **snapshots** périodiques pour ne pas tout rejouer), le **versioning de schéma d'événements** est un vrai sujet (un événement vieux de 3 ans doit toujours se décoder — d'où le codec explicite, et des stratégies d'*upcasting*), et le modèle mental est plus exigeant. À réserver aux domaines où **l'historique et l'auditabilité sont au cœur du métier** (finance, comptabilité, logistique, workflows réglementés). Pour du CRUD simple, l'agrégat état-stocké (comme `User`) reste le bon choix.

> Pour rester focalisé, `account` est **auto-contenu** (pas branché au bus/outbox). En production, on **relaie** les événements du store (idée identique à l'Outbox : un processus lit le flux append-only et publie) pour alimenter des projections et d'autres services. Ici les requêtes rejouent le flux directement (`fold on read`).

## Configuration (variables d'environnement)

| Variable       | Défaut     | Description                        |
|----------------|------------|------------------------------------|
| `HTTP_ADDR`    | `:8080`    | Adresse d'écoute                   |
| `LOG_LEVEL`    | `info`     | `debug\|info\|warn\|error`         |
| `LOG_FORMAT`   | `json`     | `json\|text`                       |
| `DB_DRIVER`    | `memory`   | `memory\|postgres`                 |
| `DATABASE_URL` | —          | Requis si `DB_DRIVER=postgres`     |

Voir `.env.example`.

## Comment étendre

**Ajouter un agrégat** : suivre le patron de `order` — un package `domain/<nom>` (agrégat + VO + events + port `Repository`), un package `application/<nom>` (use cases + ports), un repository dans `adapters/outbound/memory` et `adapters/outbound/postgres`, un handler dans `adapters/inbound/http`, puis le câbler dans `internal/bootstrap/app.go`.

**Faire réagir un agrégat à un autre** : ajouter un handler dans `internal/application/integration`, l'abonner à l'événement voulu dans la composition root (`bus.Subscribe("...", handler.Handle)`). Les agrégats concernés ne se connaissent toujours pas.

**Remplacer un adaptateur** (ex. passer à Kafka pour les événements) : implémenter `shared.EventPublisher` dans un nouveau package `adapters/outbound/kafka`, puis changer une seule ligne dans la composition root. Le domaine et l'application ne bougent pas — c'est tout l'intérêt.

**Ajouter un transport** (gRPC, CLI) : créer un adaptateur inbound qui appelle le port `application/<nom>.UseCases`. Aucune duplication de logique métier.

## Qualité de code (lint)

Une configuration [golangci-lint](https://golangci-lint.run) **v2** est fournie (`.golangci.yml`) : errcheck, govet, staticcheck, revive, gosec, gocritic, gocyclo, errorlint, bodyclose, etc.

```bash
make vet     # go vet
make lint    # golangci-lint (voir installation ci-dessous)
```

Installer golangci-lint : voir https://golangci-lint.run/usage/install/. La CI GitHub Actions lance déjà le lint via `golangci/golangci-lint-action` (job `lint`).

## Renommer le module

Le module s'appelle `github.com/example/go-ddd-hex`. Pour l'adapter à ton dépôt :

```bash
# Remplace le chemin dans go.mod et tous les imports
grep -rl 'github.com/example/go-ddd-hex' . \
  | xargs sed -i 's#github.com/example/go-ddd-hex#github.com/TON-ORG/TON-REPO#g'
go mod tidy
```

## Choix techniques

Routeur [chi](https://github.com/go-chi/chi) (léger, idiomatique), [pgx](https://github.com/jackc/pgx) pour PostgreSQL sans ORM, `log/slog` (stdlib) pour le logging structuré, `google/uuid` pour les identités, bus d'événements in-process fait maison. Migrations via [golang-migrate](https://github.com/golang-migrate/migrate), lint via [golangci-lint](https://golangci-lint.run). Aucun framework lourd : la logique métier reste indépendante de l'infrastructure.

Le montant monétaire (`Money`) est stocké en **unités mineures entières** (centimes) pour éviter les erreurs de virgule flottante, avec une devise ISO-4217 ; toute opération arithmétique vérifie la cohérence de devise.

## Observabilité

Deux signaux, tous deux obtenus **sans toucher au domaine ni aux use cases** — c'est le point.

**Logs corrélés.** Un décorateur de `slog.Handler` (`platform/logging`) lit le contexte et estampille chaque enregistrement du `request_id` de la requête. Comme le code appelait déjà `InfoContext`/`ErrorContext` partout, aucun point de log n'a été modifié. Un `X-Request-Id` entrant est conservé et renvoyé, ce qui permet de suivre une requête à travers plusieurs services.

**Métriques Prometheus**, sur `/metrics` :

```bash
curl localhost:8080/metrics
```

| Métrique | Labels | Ce qu'elle dit |
|---|---|---|
| `http_requests_total` | method, route, status | débit et taux d'erreur |
| `http_request_duration_seconds` | method, route | latence perçue |
| `domain_events_total` | event | activité **métier** : commandes soumises, utilisateurs inscrits |
| `repository_operation_duration_seconds` | port, op | où passe le temps côté sortie |
| `repository_errors_total` | port, op | pannes d'infrastructure (les `ErrNotFound` sont exclus) |

Trois mécanismes d'instrumentation, tous dans les adaptateurs :

1. **Middleware HTTP** pour les signaux RED. Le label de route est le **motif chi** (`/api/v1/orders/{id}`), jamais le chemin brut — sinon chaque identifiant créerait sa propre série et ferait exploser la cardinalité. À l'inverse, le log d'accès enregistre le chemin **brut** : un log sert à déboguer *cette* requête, une métrique à agréger *toutes* les requêtes.
2. **Décorateur de `shared.EventPublisher`** pour les métriques métier. Il enveloppe le bus et lui seul : envelopper aussi l'outbox compterait deux fois chaque événement de commande.
3. **Décorateur de port sortant** (`adapters/outbound/metrics`) pour la latence des repositories. C'est la démonstration de ce que l'hexagone permet : ajouter une préoccupation transverse en **enveloppant un port**, sans toucher une ligne de métier — le câblage se fait dans la composition root. Un seul port est décoré ; le même patron s'applique tel quel à n'importe quel autre.

Le registre Prometheus est **explicite**, créé dans la composition root et injecté, jamais `prometheus.DefaultRegisterer` : un registre global est un état partagé de processus que rien ne remet à zéro entre deux tests.

Pas de traces : elles coûteraient 6 dépendances directes et feraient passer le graphe de 22 à 61 modules (gRPC, protobuf). Les métriques disent *qu'*une chose est lente, la corrélation par `request_id` aide à savoir *laquelle*.

## Licence

[MIT](LICENSE) — © 2026 Al Moustapha. Clone, renomme, utilise en production : la seule obligation est de conserver l'avis de copyright.
