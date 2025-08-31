package main

import (
	"html/template"
	"log"
)

// parses templates into global vars
func prepareTemplates() {
	prepareTemplateBaseLogin()
	prepareTemplateBaseChat()
	prepareTemplateLogin()
	prepareTemplateChat()
}

func prepareTemplateBaseLogin() {
	templates := make([]string, 2)
	templates[0] = "./templates/base.html"
	templates[1] = "./templates/login.html"
	tmpl := template.New("base")
	t, err := tmpl.ParseFiles(templates...)
	if err != nil {
		log.Fatalf("failed parsing templates: %e", err)
	}
	baseLogin = t
}

func prepareTemplateBaseChat() {
	templates := make([]string, 2)
	templates[0] = "./templates/base.html"
	templates[1] = "./templates/chat.html"
	tmpl := template.New("base")
	t, err := tmpl.ParseFiles(templates...)
	if err != nil {
		log.Fatalf("failed parsing templates: %e", err)
	}
	baseChat = t
}

func prepareTemplateLogin() {
	tmpl := template.New("main")
	t, err := tmpl.ParseFiles("./templates/login.html")
	if err != nil {
		log.Fatalf("failed parsing templates: %e", err)
	}
	login = t
}

func prepareTemplateChat() {
	tmpl := template.New("main")
	t, err := tmpl.ParseFiles("./templates/chat.html")
	if err != nil {
		log.Fatalf("failed parsing templates: %e", err)
	}
	chat = t
}
