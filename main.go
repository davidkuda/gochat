package main

import (
	"bytes"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

var (
	clientsMu sync.Mutex
	clients   = map[*Client]bool{}
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	baseLogin *template.Template
	baseChat  *template.Template
	login     *template.Template
	chat      *template.Template
)

func main() {
	addr := flag.String("addr", "localhost:8404", "HTTP network address")

	prepareTemplates()

	// routes:
	fs := http.FileServer(http.Dir("static"))
	http.Handle("/static/", http.StripPrefix("/static/", fs))

	http.HandleFunc("/", handleLogin)
	http.HandleFunc("/login", handleLogin)
	http.HandleFunc("/chat", handleChat)
	http.HandleFunc("/ws", handleWebsocket)

	log.Println("HTTP server listening on", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

func handleLogin(w http.ResponseWriter, r *http.Request) {
	buf := bytes.Buffer{}
	err := baseLogin.ExecuteTemplate(&buf, "base", nil)
	if err != nil {
		log.Printf("failed executing template during handling %s %s: %e\n", r.Method, r.URL.Path, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	buf.WriteTo(w)
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	buf := bytes.Buffer{}

	isHTMX := r.Header.Get("HX-Request") == "true"
	log.Println(isHTMX)

	if isHTMX {
		err := chat.ExecuteTemplate(&buf, "main", nil)
		if err != nil {
			log.Printf("failed executing template during handling %s %s: %e\n", r.Method, r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	} else {
		err := baseChat.ExecuteTemplate(&buf, "base", nil)
		if err != nil {
			log.Printf("failed executing template during handling %s %s: %e\n", r.Method, r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	buf.WriteTo(w)
}

type Client struct {
	Name   string
	Gender string // Male Female Other
	Conn   *websocket.Conn
	mu     sync.Mutex
}

func broadcast(data []byte) {
	clientsMu.Lock()
	defer clientsMu.Unlock()
	for c := range clients {
		c.mu.Lock()
		defer c.mu.Unlock()
		_ = c.Conn.WriteMessage(websocket.TextMessage, data)
	}
}

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
	var msg string

	name := r.URL.Query().Get("name")
	if name == "" {
		http.Error(w, "missing ?name=...", http.StatusBadRequest)
		// TODO: render login form
		// with a toast that invalid creds
		return
	}
	gender := r.URL.Query().Get("gender")
	if name == "" {
		http.Error(w, "missing ?gender=...", http.StatusBadRequest)
		// TODO: render login form
		// with a toast that invalid creds
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}
	c := &Client{Name: name, Gender: gender, Conn: conn}

	clientsMu.Lock()
	clients[c] = true
	clientsMu.Unlock()

	// joinsTotal.WithLabelValues(c.Gender).Inc()
	msg = fmt.Sprintf("🟢 %s (%s) joined", c.Name, c.Gender)
	broadcast([]byte(msg))

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}
		// messagesTotal.WithLabelValues(c.Gender).Inc()
		// TODO:send htmx here
		msg = fmt.Sprintf("%s: %s", c.Name, string(data))
		broadcast([]byte(msg))
	}

	clientsMu.Lock()
	delete(clients, c)
	clientsMu.Unlock()
	_ = conn.Close()
	msg = fmt.Sprintf("🔴 %s (%s) left", c.Name, c.Gender)
	broadcast([]byte(msg))
}
