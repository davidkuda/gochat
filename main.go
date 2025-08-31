package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html"
	"html/template"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	clientsMu sync.Mutex
	clients   = map[*Client]bool{}
	upgrader  = websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool { return true },
	}
	baseLogin  *template.Template
	baseChat   *template.Template
	login      *template.Template
	chat       *template.Template
	partialMsg *template.Template
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
	if gender != "male" && gender != "female" && gender != "other" {
		errorMsg = errorMsg + "invalid gender"
	}
	if errorMsg != "" {
		return ChatTemplate{}, fmt.Errorf("invalid args: %v", errorMsg)
	}
	return ChatTemplate{name, gender}, nil
}

type WSMessage struct {
	Message string        `json:"message"`
	Headers WSMessageMeta `json:"HEADERS"`
}

type WSMessageMeta struct {
	HXRequest     string `json:"HX-Request"`
	HXTrigger     string `json:"HX-Trigger"`
	HXTriggerName string `json:"HX-Trigger-Name"`
	HXTarget      string `json:"HX-Target"`
	HXCurrentURL  string `json:"HX-Current-URL"`
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

type JoinRequest struct {
	Name   string `json:"name"`
	Gender string `json:"gender"`
}

type MessageTemplate struct {
	Name    string
	Message string
	Own     bool
	Avatar  string
	Time    string
}

func newMessageTemplate(clientName, name, message string) MessageTemplate {
	var own bool
	if clientName == name {
		own = true
	}
	time := time.Now().Format("15:04")

	return MessageTemplate{
		Name:    name,
		Message: message,
		Own:     own,
		Time:    time,
		Avatar:  string(name[0]),
	}
}

func msgHTML(text string) string {
	return fmt.Sprintf(`<p id="messagesContainer" class="msg" hx-swap-oob="beforeend">%s</p>`, html.EscapeString(text))
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

func broadcastHTML(fragment string) {
	for c := range clients {
		_ = c.Conn.WriteMessage(websocket.TextMessage, []byte(fragment))
	}
}

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
	var msg string

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
	frag := msgHTML(msg)
	log.Printf("sending: %s", frag)
	broadcastHTML(frag)
	broadcast([]byte(msg))

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		// messagesTotal.WithLabelValues(c.Gender).Inc()
		// TODO:send htmx here
		msg2 := fmt.Sprintf("%s: %s", c.Name, string(data))
		log.Println(msg2)

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Println("unmarshal error:", err, string(data))
			return
		}

		log.Printf("%s said: %s",
			c.Name,
			msg.Message,
		)

		t := newMessageTemplate(c.Name, c.Name, msg.Message)
		buf := bytes.Buffer{}

		err = partialMsg.ExecuteTemplate(&buf, "main", t)
		if err != nil {
			log.Println("failed executing message template:", err)
			// TODO: to tired to do error handling here, its past midnight
			return
		}

		broadcast(buf.Bytes())
	}

	clientsMu.Lock()
	delete(clients, c)
	clientsMu.Unlock()
	_ = conn.Close()
	msg = fmt.Sprintf("🔴 %s (%s) left", c.Name, c.Gender)
	broadcast([]byte(msg))
}
