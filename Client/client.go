package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/gorilla/websocket"
)

var userName string = ""

type ChatMessage struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Room string `json:"room"`
	Body string `json:"body"`
}

func connect(url string) (*websocket.Conn, error) {
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func listenServer(conn *websocket.Conn, done chan struct{}) {
	defer close(done)
	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			return
		}

		var msg ChatMessage
		err = json.Unmarshal(messageBytes, &msg)
		if err != nil {
			log.Println("JSON unmarshal error:", err)
			continue
		}

		// Only display chat_message types (adjust if you want to handle other types)
		if msg.Type == "chat_message" {
			// Compare sender username with ours
			if msg.Name != userName {
				fmt.Printf("\n[%s]: %s\n> ", msg.Name, msg.Body)
			}
			// else: skip displaying messages from ourselves to avoid duplicate echo
		} else {
			// Handle other message types if needed
			fmt.Printf("\n[Server - %s]: %s\n> ", msg.Type, string(messageBytes))
		}
	}
}

func getUserName() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Enter Username: ")
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			fmt.Print("> ")
			continue
		}
		userName = text
		fmt.Println("Welcome " + userName)
		fmt.Print("> ")
		return
	}
}

func readUserInput(conn *websocket.Conn) {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("> ")
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			fmt.Print("> ")
			continue
		}

		msg := ChatMessage{
			Type: "chat_message",
			Name: userName,
			Room: "general",
			Body: text,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			log.Println("JSON marshal error:", err)
			continue
		}

		err = conn.WriteMessage(websocket.TextMessage, data)
		if err != nil {
			log.Println("Write error:", err)
			break
		}
		fmt.Print("> ")
	}

	if err := scanner.Err(); err != nil {
		log.Println("Input error:", err)
	}
}

func waitForInterrupt(conn *websocket.Conn) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	<-interrupt
	fmt.Println("\nInterrupt received, closing connection...")
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func main() {
	getUserName()
	url := "ws://localhost:8080/ws"
	conn, err := connect(url)
	if err != nil {
		log.Fatal("Connection failed:", err)
	}
	defer conn.Close()

	fmt.Println("Connected to server at", url)

	done := make(chan struct{})
	go listenServer(conn, done)

	go readUserInput(conn)

	waitForInterrupt(conn)

	// Wait for listenServer goroutine to finish
	<-done
}
