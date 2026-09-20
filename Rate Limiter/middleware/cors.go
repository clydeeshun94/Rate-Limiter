package middleware

import "net/http"

// corsMiddleware sets CORS headers on every response.
// origin: allowed origin (e.g. "*" or specific URL)
// allowMethods: comma-separated list of allowed HTTP methods
// allowHeaders: comma-separated list of allowed request headers
// Handles preflight OPTIONS requests automatically.
func CORS(origin string, allowMethods string, allowHeaders string) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", allowMethods)
			w.Header().Set("Access-Control-Allow-Headers", allowHeaders)
			w.Header().Set("Access-Control-Allow-Credentials", "true")

			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			h.ServeHTTP(w, r)
		})
	}
}
