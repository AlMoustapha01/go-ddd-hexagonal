# DRY sur les Value Objects du domaine

**Date :** 2026-07-26
**Portée :** `internal/domain/`
**Nature :** refactor strictement iso-comportement

## Problème

Quatre packages du domaine (`user`, `order`, `account`, `shared`) répètent la même
plomberie technique. La duplication est purement mécanique : elle ne porte aucun
sens métier, mais elle impose de corriger le même bug à quatre endroits.

### Inventaire

| # | Ce qui se répète | Où | Volume |
|---|---|---|---|
| 1 | VO d'identité UUID : `struct{value uuid.UUID}` + `NewID` / `IDFromString` / `String` / `Equals` / `IsZero` | `user.ID` (dans `user.go`), `order.ID`, `order.CustomerID`, `account.ID` | 4 copies, ~20 lignes chacune |
| 2 | `func newBase(at time.Time) shared.BaseEvent` | `user/events.go`, `order/events.go`, `account/events.go` | 3 copies identiques au caractère près |
| 3 | `func (e X) AggregateID() string { return e.YID.String() }` | 5 events `order`, 3 events `account` | 8 copies |

### Hors portée — décision explicite

Deux autres duplications ont été identifiées puis **écartées volontairement** :

- **La notion de monnaie.** `order.Money` est un VO complet ; `account` refait la
  règle « code devise ISO sur 3 lettres » à la main (`account.go`, dans `Open`) et
  manipule des `int64` nus. Promouvoir `Money` dans le shared kernel recouplerait
  deux bounded contexts pour un gain de ~10 lignes. En DDD, un VO dupliqué entre
  contextes est le prix assumé de leur autonomie.
- **Les erreurs sentinelles** (`ErrNotFound` ×3, `ErrInvalidCurrency` ×2). Elles
  portent le préfixe de leur contexte (`"order: not found"`) et sont inspectées
  par `errors.Is` en dehors du domaine. Les centraliser ferait perdre cette
  information sans bénéfice.

**Principe retenu : on factorise le mécanisme technique, jamais la sémantique métier.**

## Solution

### 1. Identité — un générique à type fantôme dans le shared kernel

Nouveau fichier `internal/domain/shared/id.go` :

```go
type ID[T any] struct{ value uuid.UUID }

func NewID[T any]() ID[T]
func ParseID[T any](s string) (ID[T], error)   // -> ErrInvalidID

func (id ID[T]) String() string
func (id ID[T]) Equals(o ID[T]) bool
func (id ID[T]) IsZero() bool
```

`T` n'apparaît jamais dans le corps du struct : c'est un **type fantôme**, présent
uniquement pour que le compilateur distingue `ID[Order]` de `ID[User]`. Sans lui,
un seul type `shared.ID` laisserait passer un identifiant de commande là où on
attend un identifiant d'utilisateur.

Chaque contexte ne conserve que ce qui lui est propre — le type fantôme et **sa**
sentinelle d'erreur :

```go
// internal/domain/order/ids.go
type ID = shared.ID[Order]                     // alias : méthodes conservées

func NewID() ID { return shared.NewID[Order]() }

func IDFromString(s string) (ID, error) {
    id, err := shared.ParseID[Order](s)
    if err != nil {
        return ID{}, ErrNotFound                // le seul vrai delta par contexte
    }
    return id, nil
}

type customerTag struct{}                       // marqueur, jamais instancié
type CustomerID = shared.ID[customerTag]
```

**Deux points vérifiés par prototype compilé avant d'écrire cette spec :**

1. `type ID = shared.ID[Order]` alors que `Order` contient un champ de type `ID`
   ne produit **pas** de cycle d'instanciation — `T` n'apparaissant pas dans le
   corps du struct, l'expansion termine immédiatement.
2. La sécurité de type est réellement préservée : assigner un `CustomerID` à un
   `ID` échoue à la compilation
   (`cannot use c (variable of struct type CustomerID) as ID value`).

`user.ID`, aujourd'hui déclaré au milieu de `user.go`, est extrait vers un
`user/ids.go` dédié, par symétrie avec `order` et `account`.

### 2. `newBase` — une fabrique dans le shared kernel

`shared/events.go` gagne :

```go
func EventAt(at time.Time) BaseEvent { return BaseEvent{At: at} }
```

La forme **paramétrée** est indispensable : `user.Activate()` réutilise
`u.updatedAt` plutôt que de rappeler `time.Now()`, pour que l'horodatage de
l'agrégat et celui de l'événement coïncident exactement.

### 3. `AggregateID` — un struct d'event de base, local à chaque package

Le struct de base reste **dans son package**, il n'entre pas dans `shared` :

```go
// internal/domain/order/events.go
type base struct {
    shared.BaseEvent
    OrderID ID
}

func (e base) AggregateID() string { return e.OrderID.String() }

func newBase(id ID, at time.Time) base {
    return base{BaseEvent: shared.EventAt(at), OrderID: id}
}

type Created struct {
    base
    CustomerID CustomerID
}
```

Le nom `newBase` est réutilisé avec une signature enrichie : le vocabulaire du
repo ne change pas.

**Pourquoi local et pas dans `shared` ?** `shared.Keyed` est une interface
**optionnelle**, consommée par assertion de type dans `outbox/publisher.go` et
`application/order/projector.go`. Les events `user` ne l'implémentent
délibérément pas. Porter `AggregateID()` sur `shared.BaseEvent` rendrait tous les
events `Keyed` d'un coup et effacerait ce signal de conception — et obligerait à
ajouter un champ à sérialiser dans l'event store de `account`.

Cette asymétrie (`order` et `account` ont un `base`, `user` non) est intentionnelle
et doit être commentée dans le code.

**Vérifié :** ni `outbox/publisher.go` (qui sérialise un `envelopePayload`
explicite) ni `account/codec.go` (qui sérialise un `eventDTO` explicite) ne font
de `json.Marshal` sur les structs d'events. Changer leur structure interne est
donc sans effet sur la persistance.

## Flux de données

Les sites de construction d'événements raccourcissent :

```go
// avant
o.record(Paid{BaseEvent: newBase(o.updatedAt), OrderID: o.id})
a.raise(Deposited{BaseEvent: newBase(time.Now().UTC()), AccountID: a.id, Amount: amount, Currency: a.currency})

// après
o.record(Paid{base: newBase(o.id, o.updatedAt)})
a.raise(Deposited{base: newBase(a.id, now), Amount: amount, Currency: a.currency})
```

`account.Open` reste un cas particulier : l'agrégat n'a pas encore d'identité, il
passe `NewID()` directement à `newBase`. `account/codec.go` (Decode) construit ses
événements avec l'identité et l'horodatage relus du payload.

## Gestion d'erreurs

Invariant à préserver : `IDFromString` et `CustomerIDFromString` continuent de
retourner la sentinelle **de leur contexte** (`ErrNotFound`, `ErrInvalidCustomer`).
Aucun appelant ne change — `application/order/projector.go`,
`application/integration/customer_checker.go` et
`adapters/outbound/postgres/order_repository.go` font des `errors.Is` qui
continuent de fonctionner à l'identique.

`shared.ErrInvalidID` n'est jamais observée hors du domaine : elle existe pour
qu'un nouveau bounded context cloné depuis ce boilerplate ait un défaut sensé
avant d'avoir défini ses propres erreurs.

## Tests

Refactor iso-comportement : **aucun test n'est modifié**, ils servent d'oracle.

Baseline capturée avant modification (2026-07-26) : `go build ./...` OK,
`go vet ./...` OK, 5 packages de tests au vert
(`adapters/inbound/http`, `adapters/outbound/memory`, `adapters/outbound/outbox`,
`domain/account`, `domain/order`, `domain/user`).

> Note : le repo ne contenait pas de `go.sum`. Il a été généré par `go mod tidy`
> pour pouvoir compiler ; `go.mod` est inchangé.

Vérification après refactor :

- `make test` (`go test -race -count=1 ./...`) — doit rester identique au baseline
- `make vet`, `gofmt -s -l .` (doit ne rien retourner)
- `make lint` si `golangci-lint` est disponible
- La surface publique est inchangée (`order.ID`, `NewID`, `IDFromString`, `String`,
  `Equals`, `IsZero`, `CustomerIDFromString`). Si `application/` et `adapters/`
  compilent **sans aucune retouche**, c'est le signal principal que rien n'a fui
  du domaine.
- Un `var _ shared.Keyed = Created{}` (resp. `Opened{}`) par package documente et
  verrouille l'implémentation de l'interface optionnelle.

## Ce que ça rapporte

Le gain **n'est pas en lignes de code** — il est approximativement neutre, ce qui
est retiré des quatre packages étant réinjecté dans `shared/id.go` et dans les
commentaires pédagogiques. Le gain est :

- **~60 lignes de logique dupliquée → 0.** Faire évoluer l'identité (passer en
  UUIDv7, ajouter `MarshalText`, changer la comparaison) touche **1 fichier** au
  lieu de 4.
- **3 `newBase` identiques → 0.**
- **8 `AggregateID()` → 2**, et 8 déclarations de champ identité → 2.
- Un nouveau bounded context cloné depuis le boilerplate écrit **4 lignes**
  d'identité au lieu de 22.

## Risque assumé

Les génériques à type fantôme sont un cran au-dessus du reste du repo en niveau
d'abstraction, dans un boilerplate à vocation pédagogique. La contrepartie est un
commentaire soigné sur `shared.ID[T]` expliquant **pourquoi** le paramètre de type
existe alors qu'il n'apparaît jamais dans le struct. Dans ce repo, ce commentaire
fait partie du livrable, pas de la décoration.
