package main

import (
	"bytes"
	"encoding/json"
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

type ChatTemplate struct {
	Name   string
	Gender string
}

func newChatTemplate(name, gender string) (ChatTemplate, error) {
	var errorMsg string
	if name == "" {
		errorMsg = errorMsg + "name is an empty string;"
	}
	if len(name) > 42 {
		errorMsg = errorMsg + "name is more than 42 chars;"
	}
	log.Println(gender)
	if gender != "male" && gender != "female" && gender != "other" {
		errorMsg = errorMsg + "invalid gender"
	}
	if errorMsg != "" {
		return ChatTemplate{}, fmt.Errorf("invalid args: %v", errorMsg)
	}
	return ChatTemplate{name, gender}, nil
}

func handleChat(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	name, gender := q.Get("name"), q.Get("gender")
	t, err := newChatTemplate(name, gender)
	if err != nil {
		log.Printf("failed newChatTemplate(%s, %s) during handling %s %s: %e\n", name, gender, r.Method, r.URL.Path, err)
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		return
	}

	buf := bytes.Buffer{}

	isHTMX := r.Header.Get("HX-Request") == "true"

	if isHTMX {
		err := chat.ExecuteTemplate(&buf, "main", t)
		if err != nil {
			log.Printf("failed executing template during handling %s %s: %e\n", r.Method, r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			return
		}
	} else {
		err := baseChat.ExecuteTemplate(&buf, "base", t)
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

type JoinRequest struct {
	Name   string `json:"name"`
	Gender string `json:"gender"`
}

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
	var msg string
	log.Println("logme")

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("upgrade:", err)
		return
	}

	// Read the very first message from HTMX (the <div ws-send> payload)
	_, data, err := conn.ReadMessage()
	if err != nil {
		log.Println("read error:", err)
		return
	}

	var req JoinRequest
	if err := json.Unmarshal(data, &req); err != nil {
		log.Println("invalid json:", string(data))
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"invalid join request"}`))
		conn.Close()
		return
	}
	fmt.Println(req)

	if req.Name == "" || req.Gender == "" {
		log.Println("missing fields:", req)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"error":"missing name or gender"}`))
		conn.Close()
		return
	}

	log.Printf("✅ user joined: name=%s gender=%s\n", req.Name, req.Gender)

	c := &Client{Name: req.Name, Gender: req.Gender, Conn: conn}

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
