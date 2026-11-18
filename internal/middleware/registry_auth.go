package middleware

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
)

const (
	registryClaimsKey contextKey = "registry_claims"
	registryRouteKey  contextKey = "registry_route"
)

// RegistryVerifier is implemented by RegistryService.
type RegistryVerifier interface {
	VerifyToken(raw string) (*service.RegistryClaims, error)
}

// RegistryClaimsFromContext returns the verified token behind a /v2 request.
func RegistryClaimsFromContext(ctx context.Context) (*service.RegistryClaims, bool) {
	c, ok := ctx.Value(registryClaimsKey).(*service.RegistryClaims)
	return c, ok
}

// RegistryRouteFromContext returns the operation RegistryAuth parsed from the path, when it matched one.
func RegistryRouteFromContext(ctx context.Context) (service.RegistryRoute, bool) {
	r, ok := ctx.Value(registryRouteKey).(service.RegistryRoute)
	return r, ok
}

// WriteRegistryError writes an OCI error body; detail may be nil.
func WriteRegistryError(w http.ResponseWriter, status int, code, message string, detail any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"errors": []map[string]any{{"code": code, "message": message, "detail": detail}}})
}

// RegistryAuth checks the registry token on every /v2 request that reaches a named operation, and sets
// Docker-Distribution-API-Version on all of them. A request the token doesn't cover gets a Bearer challenge naming
// the scope it needs, as docker expects, rather than a 403.
func RegistryAuth(v RegistryVerifier, realm, serviceName string) func(http.Handler) http.Handler {
	challenge := func(w http.ResponseWriter, scope, errCode string) {
		c := `Bearer realm="` + realm + `",service="` + serviceName + `"`
		if scope != "" {
			c += `,scope="` + scope + `"`
		}
		if errCode != "" {
			c += `,error="` + errCode + `"`
		}
		w.Header().Set("WWW-Authenticate", c)
		WriteRegistryError(w, http.StatusUnauthorized, "UNAUTHORIZED", "authentication required", nil)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Docker-Distribution-API-Version", "registry/2.0")
			raw, bearer := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			var claims *service.RegistryClaims
			var verr error
			if bearer {
				claims, verr = v.VerifyToken(raw)
			}
			invalid := ""
			if bearer && verr != nil {
				invalid = "invalid_token"
			}

			rest := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/v2"), "/")
			if rest == "" {
				if claims == nil {
					challenge(w, "", invalid)
					return
				}
				next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), registryClaimsKey, claims)))
				return
			}
			route, ok := service.ParseRegistryRoute(rest)
			if !ok {
				next.ServeHTTP(w, r)
				return
			}
			if !service.ValidRegistryName(route.Name) {
				WriteRegistryError(w, http.StatusBadRequest, "NAME_INVALID", "invalid repository name", nil)
				return
			}
			action := service.RegistryActionFor(r.Method)
			if route.Op == service.RegistryOpUpload && action == service.RegistryActionDelete {
				action = service.RegistryActionPush // cancelling your own upload isn't deleting anything stored
			}
			scope := "repository:" + route.Name + ":" + action
			switch {
			case claims == nil:
				challenge(w, scope, invalid)
			case !claims.Allows(route.Name, action):
				challenge(w, scope, "insufficient_scope")
			default:
				ctx := context.WithValue(r.Context(), registryClaimsKey, claims)
				next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, registryRouteKey, route)))
			}
		})
	}
}
