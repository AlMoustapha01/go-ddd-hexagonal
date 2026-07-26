# Lot rapide : licence, readiness, cohérence des timeouts

**Date :** 2026-07-26
**Portée :** racine du repo, `internal/adapters/inbound/http/`, `internal/bootstrap/`
**Nature :** trois correctifs indépendants, regroupés parce qu'ils sont courts

## 1. Licence

### Problème

Aucun fichier `LICENSE`. Par défaut (convention de Berne), le code est donc
**tous droits réservés** : personne ne peut légalement le cloner ni le
réutiliser. Le README ouvre pourtant sur « Prêt à cloner, à renommer et à
étendre ». La promesse et le droit se contredisent.

### Solution

`LICENSE` à la racine, **MIT**, `Copyright (c) 2026 Al Moustapha`, plus une
section dans le README.

MIT plutôt qu'Apache-2.0 : c'est ce qu'attend quelqu'un qui tombe sur un
template, et Apache-2.0 imposerait par convention un en-tête dans chacun des
60+ fichiers `.go` — beaucoup de bruit pour une concession de brevets dont un
boilerplate pédagogique n'a pas l'usage.

## 2. Readiness

### Problème

`/health` renvoie 200 en dur, même avec Postgres injoignable. Sous Kubernetes,
un pod dont la base est morte reste dans le pool de load balancing et absorbe du
trafic qu'il ne peut pas servir.

### Solution

Deux sondes distinctes, comme le veut la convention Kubernetes :

- **`/health` — liveness.** Inchangé : le processus répond, donc ne le
  redémarre pas. Le README le documente déjà ainsi ; le modifier casserait une
  API publiée pour rien.
- **`/ready` — readiness.** Interroge les dépendances. 200, ou **503** — le code
  qui retire le pod du load balancer sans le tuer.

`NewServer` ne reçoit pas le pool pgx et ne doit pas l'importer : l'adaptateur
HTTP n'a pas à connaître le driver de persistance. Il déclare donc un port
étroit :

```go
// ReadinessCheck reports whether the dependencies needed to serve traffic are
// reachable. A nil check means there is nothing to check.
type ReadinessCheck func(ctx context.Context) error
```

`ServerDeps` gagne un champ `Ready ReadinessCheck`. La composition root branche
`pool.Ping` pour le driver Postgres et laisse `nil` pour la mémoire, qui n'a
aucune dépendance externe à vérifier.

La sonde s'exécute sous un contexte à 2 secondes, pour qu'une base bloquée
donne un 503 franc plutôt qu'une sonde qui pend jusqu'au timeout de
l'orchestrateur.

`/ready` reste hors du log d'accès, pour la même raison que `/health` : il est
interrogé en boucle.

## 3. Cohérence des timeouts

### Problème

`server.go` montait `middleware.Timeout(15 * time.Second)` et fixait
`WriteTimeout: 15 * time.Second`. Or le middleware `Timeout` de chi écrit son
504 dans un `defer`, donc **après** le retour du handler — au moment précis où
le délai d'écriture de la connexion expire. La réponse de timeout a toutes les
chances de ne jamais partir, et le client voit une connexion coupée au lieu d'un
504 explicite. Vérifié dans la source de chi v5.1.0 (`middleware/timeout.go`).

### Solution

Changer la valeur ne suffit pas : l'invariant doit être visible, sinon il sera
recassé. Deux constantes nommées qui l'expriment :

```go
const (
	handlerTimeout = 15 * time.Second
	// Doit DÉPASSER handlerTimeout : voir ci-dessus.
	writeTimeout = handlerTimeout + 5*time.Second
)
```

Un test assert la relation, de sorte qu'un futur ajustement des valeurs ne
puisse pas la rompre en silence.

## Tests

- `/health` répond 200 même quand la sonde de readiness échoue — c'est ce qui
  distingue liveness de readiness ;
- `/ready` répond 200 sans sonde configurée (driver mémoire) ;
- `/ready` répond 200 quand la sonde réussit ;
- `/ready` répond **503** quand elle échoue, et l'échec est loggé ;
- la sonde s'exécute bien avec une échéance ;
- ni `/health` ni `/ready` ne produisent de ligne d'accès ;
- `writeTimeout > handlerTimeout`.

### Vérification sur l'application réelle

Le cas qui compte n'est pas « la base était morte au démarrage » — le service ne
démarre alors pas du tout, `NewPool` faisant un ping — mais « la base tombe
pendant que le service tourne ». Reproduit avec un conteneur PostgreSQL 16
arrêté en cours d'exécution :

```
=== base vivante ===
  /health -> 200 {"status":"ok"}
  /ready  -> 200 {"status":"ready"}

=== base coupée ===
  /health -> 200 {"status":"ok"}          <- le process vit, ne pas le tuer
  /ready  -> 503 {"status":"unavailable"} <- le retirer du load balancer
```

```json
{"level":"WARN","msg":"readiness check failed",
 "error":"failed to connect to `user=app database=app`: ... connection refused",
 "request_id":"MacBook-Pro-de-Al.local/kzRK9jsdqP-000009"}
```

La ligne porte un `request_id` : la corrélation ajoutée précédemment couvre
aussi cette route.

## Hors périmètre

Ni métriques, ni traces, ni authentification. La couverture de la couche
application reste le chantier suivant.
