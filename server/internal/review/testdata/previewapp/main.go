// The preview integration fixture runs inside a network-isolated sandbox.
package main

import (
	"io"
	"log"
	"net/http"
	"os"
)

func main() {
	const cache = "/home/sandbox/.preview-cache/value"
	data, err := os.ReadFile(cache)
	if err != nil || string(data) != "seeded" {
		log.Fatal("preview cache was not seeded")
	}
	if err := os.WriteFile(cache, []byte("private"), 0600); err != nil {
		log.Fatal(err)
	}
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("Content-Type", "text/html")
			_, _ = io.WriteString(w, "<h1>Review candidate</h1>")
		case http.MethodPost:
			w.WriteHeader(http.StatusCreated)
			_, _ = io.Copy(w, r.Body)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	})
	log.Fatal(http.ListenAndServe("127.0.0.1:5173", nil))
}
