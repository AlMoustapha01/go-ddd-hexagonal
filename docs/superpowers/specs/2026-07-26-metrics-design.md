# Observabilité chiffrée : métriques Prometheus

**Date :** 2026-07-26
**Portée :** `internal/platform/metrics/`, `internal/adapters/`, `internal/bootstrap/`
**Nature :** ajout de fonctionnalité

## Problème

Le dépôt n'expose aucune métrique. La corrélation des logs, ajoutée
précédemment, permet de reconstituer *une* requête ; rien ne permet de répondre
à « combien », « à quelle vitesse » ou « quel taux d'erreur ».

## Choix de la pile

Mesuré plutôt qu'estimé, sur des modules jetables :

| | Dépendances directes | Modules du graphe |
|---|---|---|
| **Repo actuel** | 3 | **22** |
| API OpenTelemetry seule | +2 | 17 |
| **Prometheus `client_golang`** | **+1** | **35** |
| OpenTelemetry complet (SDK + OTLP + instrumentation HTTP) | +6 | **61** |

Prometheus est retenu. Une seule dépendance directe, un endpoint scrapable
immédiatement — on voit des chiffres dès le premier lancement — et le standard
de fait pour la collecte. OpenTelemetry complet ferait passer le graphe de 22 à
61 modules (gRPC, protobuf, grpc-gateway) dans un dépôt qui revendique trois
dépendances, et n'afficherait rien sans collecteur. L'API OTel seule est un
no-op sans SDK : elle ne produit aucun chiffre, ce qui était précisément la
demande.

Les traces ne sont pas couvertes. C'est le manque assumé de ce choix : les
métriques disent *qu'*une chose est lente, pas *où*. La corrélation par
`request_id` déjà en place comble partiellement l'écart.

### Coût réel, et une conséquence non anticipée

Mesuré après intégration, le graphe passe de **22 à 47 modules** — davantage que
les 35 du module de mesure isolé, le recouvrement avec l'existant étant moindre
qu'espéré. Une dépendance directe de plus : 3 → 4.

Surtout, `client_golang` v1.24.1 **exige Go 1.25** (comme `prometheus/common` et
`procfs`), et `go get` a donc relevé la directive `go` du module. Le `Dockerfile`
épinglait `golang:1.24-alpine` et la CI `go-version: "1.24"` : la construction
aurait cassé. Les trois épinglages (Dockerfile, CI, README) passent à 1.25.

L'alternative aurait été de figer une version périmée du client Prometheus. Pour
un dépôt dont la raison d'être est d'être cloné et étendu, livrer une dépendance
volontairement obsolète est le plus mauvais des deux conseils.

## Architecture

Le principe qui gouverne tout : **l'instrumentation ne touche ni le domaine ni
les use cases.** Trois points d'accroche, tous dans les adaptateurs ou la
composition root.

### 1. Registre explicite — `internal/platform/metrics/`

Un `*prometheus.Registry` créé dans la composition root et injecté, jamais le
registre global par défaut. Le registre global est un état partagé de processus :
deux tests qui enregistrent le même collecteur paniquent, et rien ne se remet à
zéro entre eux. Un registre explicite rend l'ensemble testable et permet
plusieurs instances dans un même binaire.

```go
type Metrics struct {
	registry     *prometheus.Registry
	HTTPRequests *prometheus.CounterVec    // method, route, status
	HTTPDuration *prometheus.HistogramVec  // method, route
	DomainEvents *prometheus.CounterVec    // event
	RepoDuration *prometheus.HistogramVec  // port, op
	RepoErrors   *prometheus.CounterVec    // port, op
}

func New() *Metrics
func (m *Metrics) Handler() http.Handler
```

### 2. Middleware HTTP — et le piège de cardinalité

Frère de `accessLog`. Le label de route est le **motif chi**, pas le chemin brut :

```go
route := chi.RouteContext(r.Context()).RoutePattern()   // "/api/v1/orders/{id}"
```

Avec `r.URL.Path`, chaque identifiant de commande créerait une série temporelle
distincte : la cardinalité explose et Prometheus s'effondre après quelques
milliers de commandes. C'est l'erreur classique de l'instrumentation HTTP.

Le contraste avec le log d'accès mérite d'être noté dans le code : **le log veut
le chemin brut** (déboguer *cette* requête), **la métrique veut le motif**
(agréger *toutes* les requêtes). Deux besoins opposés sur la même donnée.

`RoutePattern()` n'est renseigné qu'**après** le routage : il doit être lu après
`next.ServeHTTP`, pas avant.

### 3. Métriques métier — un décorateur, pas un abonné

L'idée naturelle est d'abonner un compteur au bus. Elle ne marche pas :
`Subscribe` exige un nom d'événement, donc compter *tous* les événements
imposerait d'énumérer les noms (fragile, et à maintenir à chaque ajout) ou de
modifier l'adaptateur bus.

Un décorateur de `shared.EventPublisher` compte puis délègue, sans rien changer
au bus :

```go
func NewEventPublisher(inner shared.EventPublisher, m *metrics.Metrics) shared.EventPublisher
```

**Il enveloppe le bus, et lui seul.** Envelopper aussi le publisher d'outbox
compterait deux fois chaque événement de commande : une à l'écriture dans
l'outbox, une au relais vers le bus. En enveloppant le bus on obtient exactement
un comptage par événement effectivement distribué.

### 4. Décorateur de port sortant

`internal/adapters/outbound/metrics/` enveloppe `order.Repository` pour mesurer
latence et erreurs par opération. C'est la démonstration de ce que l'hexagone
permet : ajouter une préoccupation transverse en **enveloppant un port**, sans
toucher une ligne de métier, le câblage se faisant dans la composition root.

Un seul port est décoré, commenté « le même patron s'applique à n'importe quel
port ». Le dupliquer sur les autres n'apprendrait rien de plus et doublerait le
code.

### 5. Montage

`/metrics` rejoint `/health` et `/ready` : hors du log d'accès **et** hors du
middleware de métriques — un scrape toutes les quinze secondes n'a pas à se
compter lui-même, ni à polluer les latences observées.

`ServerDeps` gagne un champ `Metrics` optionnel. Nil désactive proprement le
middleware et l'endpoint, ce qui n'est pas une commodité de test mais une
nécessité : les tests de sondes existants construisent `ServerDeps{}` sans
métriques.

## Gestion d'erreurs

Aucun chemin d'instrumentation ne peut faire échouer une requête ou une
commande. Les décorateurs mesurent puis renvoient tel quel le résultat du
composant enveloppé — y compris son erreur, non transformée, pour que les
`errors.Is` en amont continuent de fonctionner.

## Tests

- **le middleware enregistre le motif de route, jamais le chemin brut** — le
  garde-fou anti-cardinalité, dont on vérifiera qu'il échoue si l'on repasse à
  `r.URL.Path` ;
- labels méthode et statut corrects, durée observée ;
- le décorateur d'événements compte, délègue, et **propage l'erreur de l'inner** ;
- le décorateur de repository mesure, compte les erreurs, et **renvoie le même
  résultat que le port enveloppé** ;
- `/metrics` sert de l'exposition Prometheus et ne produit aucune ligne d'accès ;
- un serveur sans métriques câblées fonctionne (`ServerDeps.Metrics` nil).

Le garde-fou anti-cardinalité a été **vérifié capable d'échouer** : en repassant
à `r.URL.Path`, les deux tests tombent en nommant les identifiants qui fuient
(« three requests down one route produced 3 time series »).

### Vérification sur l'application réelle

Trafic généré contre `go run ./cmd/api` (driver mémoire) : création d'un
utilisateur, activation, création d'une commande, ajout de ligne, soumission,
trois lectures et une lecture avec identifiant malformé.

```
domain_events_total{event="order.created"}    1
domain_events_total{event="order.line_added"} 1
domain_events_total{event="order.placed"}     1
domain_events_total{event="user.activated"}   1
domain_events_total{event="user.registered"}  1

http_requests_total{method="GET",route="/api/v1/orders/{id}",status="200"} 3
http_requests_total{method="GET",route="/api/v1/orders/{id}",status="404"} 1

repository_operation_duration_seconds_count{op="FindByID",port="order.Repository"} 5
repository_operation_duration_seconds_count{op="Save",port="order.Repository"}     3
```

Trois lectures et deux identifiants distincts se replient sur **deux séries**,
pas cinq : le label de route tient.

Chaque événement est compté exactement une fois, alors que les événements de
commande transitent par outbox → relais → bus.

Et une propriété se lit directement dans les chiffres : `FindByID` vaut 5 (deux
commandes plus trois reprojections) et `Save` 3, mais **les trois lectures
`GET /orders/{id}` n'apparaissent nulle part** — elles sont servies par le read
model et ne touchent jamais le repository d'écriture. La séparation CQRS est
visible dans les métriques.

## Hors périmètre

Ni traces, ni service Prometheus dans `docker-compose.yml`, ni bascule de
configuration : `/metrics` est trop peu coûteux pour mériter un drapeau. Le
README gagne une section.
