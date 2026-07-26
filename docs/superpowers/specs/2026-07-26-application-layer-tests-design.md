# Tests de la couche application

**Date :** 2026-07-26
**Portée :** `internal/application/`, `Makefile`
**Nature :** ajout de tests + correction de l'outillage de couverture

## Correction préalable

Un audit antérieur de ce dépôt annonçait « couche application : 0 % ». **C'était
faux**, et la cause mérite d'être notée parce qu'elle est instructive.

`go test -cover ./...` crédite chaque package de la couverture produite par ses
**propres** tests. La couche application n'a pas de tests à elle, mais elle est
massivement traversée par ceux de l'adaptateur HTTP (`TestOrderLifecycle`,
`TestCQRSReadModelReflectsWrites`, `TestDeactivatingUserCancelsOpenOrders`…).
Mesurée correctement, avec `-coverpkg` :

```
go test -coverpkg=./internal/application/... ./...
total: 70.4%
```

Le diagnostic change donc de nature : le problème n'est pas l'absence de
couverture, c'est que **tout ce qui est couvert l'est par le chemin nominal**,
piloté depuis le transport.

## Ce qui manque réellement

| Zone | Couverture actuelle |
|---|---|
| `user.Rename`, `user.List`, `user.Delete` | **0 %** — jamais exécutées |
| `IsActiveCustomer` (pont inter-contextes) | 55,6 % |
| `projector.Handle` | 60 % |
| `clampPage` | 60 % |
| `user.dispatch` | 60 % |
| `IsValidationError` (les trois packages) | 66,7 % |
| `user.mutate` / `order.mutate` | 66,7 % / 63,6 % |
| `order.commit` | 70 % |
| `account.execute` | 78,6 % |

Autrement dit : **toutes les branches d'erreur**, toutes les tables de
classification, et tous les cas limites.

Et un manque qu'aucun pourcentage ne révèle : **rien n'assert que `repo.Save` et
`outbox.Publish` ont lieu dans le même `uow.Within`**. C'est le contrat du
pattern Outbox au niveau applicatif, et il n'est aujourd'hui vérifié nulle part.

## Stratégie de doublures

**Les adaptateurs `memory` servent de collaborateurs réels.** Depuis les tests
de contrat, ils rejouent la même suite que Postgres : ce ne sont plus des
fictions qu'on espère justes, mais des implémentations prouvées équivalentes.
Les utiliser pour l'état et les chemins nominaux donne des tests qui portent sur
du comportement réel plutôt que sur des mocks qui dérivent. C'est le dividende
direct du travail précédent.

**De petites doublures dédiées, uniquement pour injecter des pannes** : un
repository qui échoue, un publisher qui échoue, un dispatcher qui échoue. Elles
existent pour atteindre des branches qu'aucune implémentation correcte ne peut
produire.

Pas de bibliothèque de mocks : elle ajouterait une dépendance et de la
génération de code à un dépôt qui en revendique trois.

## L'atomicité de l'Outbox

`memory.Transactor` est un no-op documenté ; il ne peut rien prouver. Les tests
utilisent un `shared.Atomic` **enregistreur** qui note ce qui s'est produit à
l'intérieur du callback :

```go
type recordingUoW struct {
    depth     int   // > 0 pendant l'exécution du callback
    committed bool
}
```

Les doublures de repository et de publisher lui demandent « suis-je dans une
transaction ? » au moment de leur appel. Le test assert alors que **les deux**
écritures ont eu lieu à l'intérieur, et qu'une erreur de l'une empêche le commit.

C'est le bon niveau d'assertion ici. Le travail de la couche application est
d'**appeler** `Within` et d'y placer les deux écritures ; que la transaction
soit réelle est le travail de l'adaptateur — déjà prouvé par
`TestUnitOfWorkRollsBackAggregateAndOutboxTogether` contre un vrai PostgreSQL.
Les deux tests ensemble couvrent le contrat ; aucun ne suffit seul.

## Contenu par package

**`user`** — Register (email déjà pris → `ErrEmailTaken`, email invalide, nom
vide), Activate/Deactivate (invariants remontés), Rename, GetByID, List avec
bornage (limite ≤ 0 et > 100 → 20 ; offset négatif → 0), Delete, un publisher en
échec qui **ne doit pas** invalider une écriture déjà persistée, et
`IsValidationError` en table.

**`order`** — le contrat d'atomicité ci-dessus ; client inactif ou inexistant →
`ErrCustomerNotActive` ; erreur du `CustomerChecker` propagée ; dispatch inline
en échec qui n'échoue pas la commande (best-effort) ; `mutate` sur identifiant
malformé et sur commande inconnue ; `IsValidationError` en table.

**`order.QueryService`** — bornage de `clampPage`, délégation au ReadStore.

**`order.Projector`** — reprojection après événement ; **projection supprimée
quand l'agrégat n'existe plus** ; événement non-`Keyed` ignoré ; identifiant non
parsable ignoré ; erreur du ReadStore propagée.

**`account`** — la boucle load → decide → append ; `expectedVersion` correct
(sinon deux commandes successives échoueraient) ; conflit de concurrence remonté
tel quel pour que le transport le traduise en 409 ; solde insuffisant ;
`IsValidationError` en table.

**`integration`** — `IsActiveCustomer` : id malformé → `(false, nil)` **sans
erreur**, utilisateur inconnu → `(false, nil)`, erreur d'infrastructure
propagée, utilisateur actif/inactif. `CancelOrdersOnUserDeactivation` : ignore
un événement d'un autre type, ne touche que les commandes `draft` et `placed`,
**poursuit** malgré l'échec d'une annulation, propage une erreur de lecture.
Plus un test de la cascade réelle avec les vrais services order câblés sur les
adaptateurs mémoire — sous la couche HTTP, pour qu'un échec se localise mieux
que dans le test de bout en bout existant.

## Outillage

`make cover` produit aujourd'hui le chiffre trompeur qui a causé l'erreur de
diagnostic initiale. Il passe à `-coverpkg=./internal/...`, de sorte que la
couverture affichée soit celle du code réellement exercé, quel que soit le
package d'où part le test.

## Vérification

Résultats mesurés.

Couverture de `internal/application/...`, mesurée des deux côtés avec la même
commande (`-coverpkg=./internal/application/... ./...`) :

| | Avant | Après |
|---|---|---|
| Total | 70,4 % | **98,3 %** |

Il ne reste que quatre fonctions sous 100 %, toutes au-dessus de 90 % :
`projector.Handle` (93,3 %), `order.mutate` (90,9 %), `user.List` (90,9 %),
`user.mutate` (91,7 %) — des branches d'échec d'écriture dont le coût de mise en
scène dépasse la valeur. La poursuite du 100 % n'est pas un objectif.

Couverture globale de `internal/...` : **66,0 %**.

- `go test -race -count=1 ./...` : **13 packages au vert** ;
- `go vet`, `gofmt -s`, `golangci-lint run ./...` : 0 issue.

### Aucun défaut trouvé

Contrairement aux tests de contrat des adaptateurs — qui avaient révélé un
500-au-lieu-de-404, cinq slices `nil` et un tri manquant — **ces tests n'ont
mis au jour aucun bug**. Le code applicatif était déjà correct ; il n'était
simplement épinglé par rien. C'est un résultat en soi, et il vaut mieux le dire
que fabriquer une trouvaille.

Une affirmation portée par un commentaire de test a tout de même été vérifiée
plutôt que supposée : `ErrConcurrencyConflict` est bien traduit en 409 par le
transport (`account_handler.go:102`), et non absorbé dans le 400 des erreurs de
validation.

## Hors périmètre

Ni authentification, ni métriques, ni traces. Les tests HTTP existants ne sont
pas modifiés : ils couvrent le bout en bout, et ces tests-ci couvrent les
branches qu'ils ne peuvent pas atteindre.
