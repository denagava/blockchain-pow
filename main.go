package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/denagava/blockchain-pow/chain"
)

const mineTimeout = 60 * time.Second

type api struct {
	bc   *chain.Blockchain
	path string
}

func (a *api) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /blocks", a.handleBlocks)
	mux.HandleFunc("POST /mine", a.handleMine)
	mux.HandleFunc("GET /validate", a.handleValidate)
	return mux
}

func (a *api) handleBlocks(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, a.bc.Blocks())
}

func (a *api) handleMine(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Data string `json:"data"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Data == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"data\": \"...\"}"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), mineTimeout)
	defer cancel()
	start := time.Now()
	b, err := a.bc.Mine(ctx, req.Data)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "mining aborted: " + err.Error()})
		return
	}
	log.Printf("mined block #%d nonce=%d in %s", b.Index, b.Nonce, time.Since(start).Round(time.Millisecond))

	if err := a.bc.Save(a.path); err != nil {
		log.Printf("save chain: %v", err)
	}
	writeJSON(w, http.StatusCreated, b)
}

func (a *api) handleValidate(w http.ResponseWriter, _ *http.Request) {
	idx, err := a.bc.Validate()
	resp := map[string]any{"is_valid": err == nil}
	if err != nil {
		resp["invalid_block"] = idx
		resp["reason"] = err.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address")
	path := flag.String("data", "blockchain.json", "file to persist the chain")
	difficulty := flag.Int("difficulty", 4, "number of leading zero hex digits required (for a new chain)")
	flag.Parse()

	bc, err := chain.Load(*path, *difficulty)
	if err != nil {
		log.Fatal(err)
	}
	a := &api{bc: bc, path: *path}

	srv := &http.Server{Addr: *addr, Handler: a.routes(), ReadHeaderTimeout: 5 * time.Second}
	go func() {
		log.Printf("listening on %s, difficulty=%d, blocks=%d", *addr, bc.Difficulty(), len(bc.Blocks()))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	if err := bc.Save(*path); err != nil {
		log.Printf("save chain: %v", err)
	}
	log.Println("stopped")
}
