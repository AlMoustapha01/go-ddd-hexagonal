# Corrélation des logs par request id

**Date :** 2026-07-26
**Portée :** `internal/platform/logging/`, `internal/adapters/inbound/http/`
**Nature :** ajout d'observabilité

## Problème

`middleware.RequestID` est monté dans `server.go` — et c'est sa **seule** occurrence
dans tout le code. L'identifiant qu'il génère n'atteint jamais `slog`. En
production, rien ne permet de recoller les lignes de log d'une même requête.

Deux manques distincts, en réalité :

1. **Aucune corrélation.** Les 15 points de log du projet n'émettent pas de
   `request_id`, donc deux erreurs concomitantes sont indiscernables.
2. **Aucune trace de requête.** `middleware.Logger` de chi n'est pas monté : une
   requête qui réussit ne produit *aucune* ligne. Il n'y a donc souvent rien à
   corréler.

## Ce qui est déjà en place

Le point important, et il est favorable : **le code appelle déjà les variantes
`*Context` partout**. Sur 16 points de log, 12 utilisent `InfoContext`,
`ErrorContext` ou `WarnContext` en passant le contexte de la requête, propagé
jusque dans la couche application et les adaptateurs sortants. Les 4 restants
sont dans `main.go` et `bootstrap`, hors de toute requête, où c'est correct.

Le travail difficile — faire descendre le contexte à travers les couches — est
donc déjà fait. Ce qui manque est que `slog.NewJSONHandler` **ignore le
contexte** qu'on lui passe.

## Solution

### 1. Un décorateur de `slog.Handler`

`internal/platform/logging/context.go` porte la corrélation, sans dépendance
externe :

```go
type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context
func RequestID(ctx context.Context) string
```

`internal/platform/logging/handler.go` décore le handler de base :

```go
type contextHandler struct{ slog.Handler }

func (h contextHandler) Handle(ctx context.Context, rec slog.Record) error {
	if id := RequestID(ctx); id != "" {
		rec.AddAttrs(slog.String("request_id", id))
	}
	return h.Handler.Handle(ctx, rec)
}

func (h contextHandler) WithAttrs(a []slog.Attr) slog.Handler { return contextHandler{h.Handler.WithAttrs(a)} }
func (h contextHandler) WithGroup(n string) slog.Handler      { return contextHandler{h.Handler.WithGroup(n)} }
```

Les deux dernières méthodes ne sont pas décoratives : `slog.Handler.WithAttrs`
renvoie un **nouveau** handler, et sans redéclaration ce serait le handler
sous-jacent nu. Le premier `log.With(...)` ferait alors disparaître la
corrélation en silence, sans erreur ni signe visible. Un test verrouille ce
comportement.

`New()` enveloppe le handler choisi : `slog.New(contextHandler{handler})`.

**Aucun des 15 points de log existants n'est modifié.** C'est exactement l'usage
pour lequel les variantes `*Context` de slog existent.

`New` écrivait sur `os.Stdout` en dur, ce qui rend la chaîne complète
intestable. Elle délègue désormais à `NewTo(w io.Writer, level, format)`, qui
prend la destination en paramètre. Ce n'est pas une couture réservée aux tests :
c'est ce qui permet aussi d'écrire ailleurs (un fichier) sans réassembler un
logger en parallèle, qui dériverait de celui-ci.

### 2. Deux middlewares HTTP

`internal/adapters/inbound/http/middleware.go`.

**`requestContext`** fait la passerelle et l'écho :

```go
id := middleware.GetReqID(r.Context())
w.Header().Set("X-Request-Id", id)
next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
```

Il vit dans l'adaptateur HTTP et non dans `platform/logging`, parce que chi est
un détail de transport que `platform` ne doit pas importer. Ce middleware est la
couche anti-corruption qui traduit la clé de contexte de chi vers celle de la
plateforme.

L'écho en réponse permet à un client de citer l'identifiant dans un rapport de
bug. La propagation entrante est gratuite : `middleware.RequestID` de chi honore
déjà un en-tête `X-Request-Id` reçu et ne génère un identifiant que sinon.

**`accessLog`** émet la ligne d'ancrage — méthode, chemin, statut, octets, durée
— en s'appuyant sur `middleware.NewWrapResponseWriter` de chi pour capter le
statut et la taille. Aucune dépendance nouvelle.

### 3. Montage

```go
r.Use(middleware.RequestID)   // génère, ou honore un X-Request-Id entrant
r.Use(requestContext)         // doit suivre RequestID
r.Use(middleware.RealIP)
r.Use(middleware.Recoverer)
r.Use(middleware.Timeout(15 * time.Second))

r.Get("/health", healthCheck)          // délibérément hors du log d'accès

r.Route("/api/v1", func(api chi.Router) {
	api.Use(accessLog(log))
	...
})
```

Le log d'accès est monté sur le groupe `/api/v1` plutôt que globalement : une
sonde de liveness interrogée toutes les 10 secondes produirait environ 8 600
lignes par jour de pur bruit. Le placement suffit — pas besoin d'une liste
d'exclusion à maintenir.

## Flux

```
X-Request-Id entrant (ou identifiant généré)
   └─ middleware.RequestID  → contexte chi
        └─ requestContext   → contexte plateforme + en-tête de réponse
             ├─ accessLog                     → "request completed" + request_id
             └─ handler → use case → domaine  → tout log *Context + request_id
```

Résultat pour une requête :

```json
{"level":"INFO","msg":"request completed","method":"POST","path":"/api/v1/orders",
 "status":201,"bytes":247,"duration_ms":12,"request_id":"api-7f2/Ab3kQ9-000042"}
{"level":"INFO","msg":"domain event","event":"order.created",
 "request_id":"api-7f2/Ab3kQ9-000042"}
```

## Gestion d'erreurs

Le décorateur ne peut pas échouer : en l'absence d'identifiant dans le contexte
il n'ajoute **rien** — pas de champ vide parasite dans les logs de démarrage ou
des tâches de fond. Il délègue ensuite au handler sous-jacent sans modifier son
erreur.

`accessLog` logge dans un `defer`, donc une requête qui panique produit quand
même sa ligne : `middleware.Recoverer` est monté en amont et convertit la panique
en 500, que le `defer` observe.

## Tests

Dans le style du repo, sur un `slog` écrivant en JSON dans un `bytes.Buffer` :

- `request_id` présent quand le contexte le porte ;
- **absent** quand il ne le porte pas (aucun champ vide) ;
- **la corrélation survit à `log.With(...)` et à `WithGroup`** — le piège de
  `WithAttrs`. Vérifié capable d'échouer : en retirant les deux surcharges, les
  tests tombent sur `request_id lost through With: got <nil>` ;
- `New` renvoie bien un logger décoré (garde-fou du câblage lui-même : oublier
  d'envelopper laisserait tous les autres tests au vert et les logs de
  production nus) ;
- le middleware pose l'identifiant dans le contexte et l'écho dans l'en-tête ;
- un `X-Request-Id` entrant est conservé tel quel ;
- la ligne d'accès porte méthode, chemin, statut, octets et durée ;
- **de bout en bout** : la ligne d'accès et un log émis plus profond pendant la
  même requête portent le même identifiant ;
- `/health` ne produit aucune ligne.

Vérifié aussi sur l'application réelle (`go run ./cmd/api`, driver mémoire) :

```json
{"level":"INFO","msg":"domain event","event":"user.registered",
 "request_id":"MacBook-Pro-de-Al.local/gXdQvTgquY-000002"}
{"level":"INFO","msg":"request completed","method":"POST","path":"/api/v1/users",
 "status":201,"bytes":190,"duration_ms":0,
 "request_id":"MacBook-Pro-de-Al.local/gXdQvTgquY-000002"}
```

L'événement de domaine est émis par `memory/event_publisher.go`, au fond d'un
adaptateur sortant qui ne connaît que le contexte. Un `X-Request-Id: trace-abc-123`
fourni par l'appelant traverse la chaîne intacte et revient dans l'en-tête de
réponse ; les lignes de démarrage n'ont pas de champ `request_id`.

## Hors périmètre

`WriteTimeout` égal au timeout applicatif dans le même fichier (`server.go`) :
défaut réel et déjà diagnostiqué, mais distinct — il mérite son propre commit.
Ni métriques, ni traces OpenTelemetry.
