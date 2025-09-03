// TODO:
// - [ ] what happens if two people use the same name? probably just an error in the map, but needs handling.
// - [ ] precompute partial templates

package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

var (
	// Prometheus metrics (with gender label)
	chatRendersTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_chat_renders_total",
			Help: "total number of times the chat is rendered and sent to a client",
		},
		// success or error
		[]string{"status"},
	)
	joinsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_joins_total",
			Help: "Total number of chat joins, labeled by gender.",
		},
		[]string{"gender"},
	)
	usersOnline = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "gochat_users_online",
			Help: "current number of users that are online in the chat",
		},
		[]string{"gender"},
	)
	messagesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "gochat_messages_total",
			Help: "Total number of chat messages, labeled by gender.",
		},
		[]string{"gender"},
	)
	broadcastMessageDuration = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name: "gochat_broadcast_duration_seconds",
			Help: "duration of a sending a message to each connected client",
		},
	)
)

func main() {
	chatRendersTotal.WithLabelValues("success")
	chatRendersTotal.WithLabelValues("error")
	joinsTotal.WithLabelValues("male")
	joinsTotal.WithLabelValues("female")
	joinsTotal.WithLabelValues("other")
	usersOnline.WithLabelValues("male")
	usersOnline.WithLabelValues("female")
	usersOnline.WithLabelValues("other")
	messagesTotal.WithLabelValues("male")
	messagesTotal.WithLabelValues("female")
	messagesTotal.WithLabelValues("other")

	// Register metrics and expose /metrics
	// instead of global registry, create your own (to prevent recording go_ process_ and httprom_ metrics)
	reg := prometheus.NewRegistry()
	reg.MustRegister(
		chatRendersTotal,
		joinsTotal,
		usersOnline,
		messagesTotal,
		// broadcastMessageDuration,
	)
	http.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))

	addr := flag.String("addr", "localhost:8404", "HTTP network address")
	flag.Parse()

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
	Name    string        `json:"name"`
	Gender  string        `json:"gender"`
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
		chatRendersTotal.WithLabelValues("error").Inc()
		return
	}

	buf := bytes.Buffer{}

	isHTMX := r.Header.Get("HX-Request") == "true"

	if isHTMX {
		err := chat.ExecuteTemplate(&buf, "main", t)
		if err != nil {
			log.Printf("failed executing template during handling %s %s: %e\n", r.Method, r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			chatRendersTotal.WithLabelValues("error").Inc()
			return
		}
	} else {
		err := baseChat.ExecuteTemplate(&buf, "base", t)
		if err != nil {
			log.Printf("failed executing template during handling %s %s: %e\n", r.Method, r.URL.Path, err)
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			chatRendersTotal.WithLabelValues("error").Inc()
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	buf.WriteTo(w)
	chatRendersTotal.WithLabelValues("success").Inc()
}

type Client struct {
	Name      string
	Gender    string // Male Female Other
	Joined    time.Time
	JoinedStr string
	Left      time.Time
	LeftStr   string
	Conn      *websocket.Conn
	mu        sync.Mutex
}

type JoinRequest struct {
	Name   string `json:"name"`
	Gender string `json:"gender"`
}

type NewUserTemplate struct {
	Name   string
	Gender string
	Time   string
}

func newNewUserTemplate(name, gender string) NewUserTemplate {
	time := time.Now().Format("15:04")
	return NewUserTemplate{
		Name:   name,
		Gender: gender,
		Time:   time,
	}
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
	avatar := strings.ToUpper(string(name[0]))
	return MessageTemplate{
		Name:    name,
		Message: message,
		Own:     own,
		Time:    time,
		Avatar:  avatar,
	}
}

func userCountHTML(n int) string {
	txt := fmt.Sprintf("%d user", n)
	if n != 1 {
		txt += "s"
	}
	txt += " online"
	return fmt.Sprintf(`<span id="userCount" hx-swap-oob="outerHTML" class="user-count">%s</span>`, txt)
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

func broadcastMessage(msg WSMessage) {
	start := time.Now()

	clientsMu.Lock()
	defer clientsMu.Unlock()
	for c := range clients {
		c.mu.Lock()
		defer c.mu.Unlock()

		t := newMessageTemplate(c.Name, msg.Name, msg.Message)
		buf := bytes.Buffer{}

		err := partialMsg.ExecuteTemplate(&buf, "main", t)
		if err != nil {
			log.Println("failed executing message template:", err)
			// TODO: to tired to do error handling here, its past midnight
			return
		}

		_ = c.Conn.WriteMessage(websocket.TextMessage, buf.Bytes())
	}

	// Observe latency
	// optional: simulate heavy work:
	// time.Sleep(time.Duration(300 * time.Millisecond))
	broadcastMessageDuration.Observe(time.Since(start).Seconds())
}

func handleWebsocket(w http.ResponseWriter, r *http.Request) {
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
	usersOnline.WithLabelValues(req.Gender).Inc()
	joinsTotal.WithLabelValues(req.Gender).Inc()

	now := time.Now()
	c := &Client{
		Name:      req.Name,
		Gender:    req.Gender,
		Conn:      conn,
		Joined:    now,
		JoinedStr: now.Format("15:04"),
	}

	clientsMu.Lock()
	clients[c] = true
	clientsMu.Unlock()

	newMemberTemplate := prepareTemplatePartialNewMember()
	buf := bytes.Buffer{}
	err = newMemberTemplate.ExecuteTemplate(&buf, "main", c)
	if err != nil {
		log.Println("failed executing message template:", err)
		// TODO: to tired to do error handling here, its past midnight
		return
	}
	broadcast(buf.Bytes())

	frag := userCountHTML(len(clients))
	broadcast([]byte(frag))

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			break
		}

		var msg WSMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			log.Println("unmarshal error:", err, string(data))
			return
		}

		broadcastMessage(msg)
		messagesTotal.WithLabelValues(msg.Gender).Inc()
	}

	clientsMu.Lock()
	delete(clients, c)
	clientsMu.Unlock()
	_ = conn.Close()

	now = time.Now()
	c.Left = now
	c.LeftStr = now.Format("15:04")
	bye := prepareTemplatePartialByeMember()
	byebuf := bytes.Buffer{}
	err = bye.ExecuteTemplate(&byebuf, "main", c)
	if err != nil {
		log.Println("failed executing message template:", err)
		// TODO: to tired to do error handling here, its past midnight
		return
	}
	broadcast(byebuf.Bytes())
	newUserCount := userCountHTML(len(clients))
	broadcast([]byte(newUserCount))
	log.Printf("👋 bye user! name=%s gender=%s\n", c.Name, c.Gender)
	usersOnline.WithLabelValues(c.Gender).Dec()
}
