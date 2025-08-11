package main

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/gorilla/websocket"
)

var userName string = ""
var pub_key *rsa.PublicKey
var priv_key *rsa.PrivateKey

type MsgType struct {
	Type string `json:"type"`
}

// struct of a chat message
type ChatMessage struct {
	Type     string `json:"type"`
	UserName string `json:"userame"`
	Room     string `json:"room"`
	Body     string `json:"body"`
}

// struct of a join request -> sent when a user joins the server for the first time
type JoinRequest struct {
	Type     string `json:"type"`
	UserName string `json:"username"`
	Pub_Key  string `json:"pub_key"`
}

type Config struct {
	Username   string `json:"username"`
	ServerAddr string `json:"server_addr"`
}

func getConfigFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	appDir := filepath.Join(configDir, "ghostwire")
	os.MkdirAll(appDir, 0700)
	return appDir, nil
}

func saveConfig(cfg Config) error {
	path, err := getConfigFilePath()
	path = filepath.Join(path, "config.json")
	if err != nil {
		return err
	}

	file, err := os.Create(path)
	if err != nil {
		return err
	}

	defer file.Close()
	encoder := json.NewEncoder(file)
	return encoder.Encode(cfg)
}

func loadConfig(filePath string) (Config, error) {
	fmt.Println("Reading config file")
	var cfg Config

	path := filepath.Join(filePath, "config.json")

	file, err := os.Open(path)
	if err != nil {
		fmt.Println("Error loading config: " + err.Error())
		return cfg, err
	}
	fmt.Println("Loaded the config file")
	defer file.Close()

	decoder := json.NewDecoder(file)
	err = decoder.Decode(&cfg)
	fmt.Print("Decoded the file: ")
	fmt.Println(cfg)
	userName = cfg.Username
	if err != nil {
		fmt.Println("Error decoding config: " + err.Error())
	}
	fmt.Println("Returning out of loadConfig()")
	return cfg, err
}

func connect(url string) (*websocket.Conn, error) {
	fmt.Println("Connecting to server at " + url)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

func listenServer(conn *websocket.Conn, done chan struct{}, priv_key *rsa.PrivateKey) {
	defer close(done)
	for {
		_, messageBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("Read error:", err)
			return
		}

		var mt MsgType
		err = json.Unmarshal(messageBytes, &mt)
		if err != nil {
			// handle error
		}
		print(mt.Type)

		switch mt.Type {
		case "chat_message":
			var msg ChatMessage
			err = json.Unmarshal(messageBytes, &msg)
			if err != nil {
				log.Println("JSON unmarshal error:", err)
				continue
			}

			// Only display chat_message types (adjust if you want to handle other types)
			if msg.Type == "chat_message" {
				// Compare sender username with ours
				if msg.UserName != userName {
					fmt.Printf("\n[%s]: %s\n> ", msg.UserName, msg.Body)
				}
				// else: skip displaying messages from ourselves to avoid duplicate echo
			} else {
				// Handle other message types if needed
				fmt.Printf("\n[Server - %s]: %s\n> ", msg.Type, string(messageBytes))
			}

		case "join_approve":
			fmt.Println("approved")
		}
	}
}

func getUserName() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Enter Username: ")
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}
		userName = text
		fmt.Println("Welcome " + userName)
		fmt.Print("> ")
		return
	}
}

func getServerAddress() string {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Print("Enter Server Address: ")
	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			fmt.Print("> ")
			continue
		}
		return text
	}
	return "we have a problem"
}

func sendJoinRequest(conn *websocket.Conn, pubkey *rsa.PublicKey) {
	log.Println("Sending join request")
	Pub_Key_Pem, err := publicKeyToPEM(pubkey)
	if err != nil {
		log.Println("JSON marshal error in sending message:", err)
		return
	}

	request := JoinRequest{
		Type:     "join_request",
		UserName: userName,
		Pub_Key:  Pub_Key_Pem,
	}

	request.UserName = userName

	data, err := json.Marshal(request)
	if err != nil {
		log.Println("JSON marshal error in sending message:", err)
		return
	}

	log.Println("Join request: " + string(data))

	err = conn.WriteMessage(websocket.TextMessage, data)
	if err != nil {
		log.Println("Write error:", err)
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
			Type:     "chat_message",
			UserName: userName,
			Room:     "general",
			Body:     text,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			log.Println("JSON marshal error in sending message:", err)
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

// generateRSAKeys generates an RSA keypair and returns *rsa.PrivateKey
func generateRSAKeys() (*rsa.PrivateKey, error) {
	return rsa.GenerateKey(rand.Reader, 2048)
}

// savePrivateKey saves an RSA private key to a PEM file
func savePrivateKey(filePath string, key *rsa.PrivateKey) error {
	// Convert to PKCS#1 ASN.1 DER encoded form
	privDER := x509.MarshalPKCS1PrivateKey(key)

	// Create PEM block
	privBlock := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: privDER,
	}

	// Write to file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	return pem.Encode(file, privBlock)
}

// savePublicKey saves an RSA public key to a PEM file
func savePublicKey(filePath string, pubkey *rsa.PublicKey) error {
	// Convert to PKIX ASN.1 DER form
	pubDER, err := x509.MarshalPKIXPublicKey(pubkey)
	if err != nil {
		return err
	}

	// Create PEM block
	pubBlock := &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubDER,
	}

	// Write to file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	return pem.Encode(file, pubBlock)
}

// loadPrivateKey loads an RSA private key from a PEM file
func loadPrivateKey(filePath string) (*rsa.PrivateKey, error) {
	filePath = filepath.Join(filePath, "private_key.pem")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "RSA PRIVATE KEY" {
		return nil, errors.New("failed to decode PEM block containing private key")
	}

	key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return key, nil
}

// loadPublicKey loads an RSA public key from a PEM file
func loadPublicKey(filePath string) (*rsa.PublicKey, error) {
	filePath = filepath.Join(filePath, "public_key.pem")

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	block, _ := pem.Decode(data)
	if block == nil || block.Type != "PUBLIC KEY" {
		return nil, errors.New("failed to decode PEM block containing public key")
	}

	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}

	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("not an RSA public key")
	}

	return rsaPub, nil
}

// publicKeyToPEM converts an RSA public key to a PEM-encoded string
func publicKeyToPEM(pubkey *rsa.PublicKey) (string, error) {
	pubASN1, err := x509.MarshalPKIXPublicKey(pubkey)
	if err != nil {
		return "", err
	}

	var pemBuf bytes.Buffer
	err = pem.Encode(&pemBuf, &pem.Block{
		Type:  "PUBLIC KEY",
		Bytes: pubASN1,
	})
	if err != nil {
		return "", err
	}

	return pemBuf.String(), nil
}

func waitForInterrupt(conn *websocket.Conn) {
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt, syscall.SIGTERM)

	<-interrupt
	fmt.Println("\nInterrupt received, closing connection...")
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}

func main() {

	//used to determine if we need to send a join request
	var newClient = false

	//get the path to save the
	filePath, err := getConfigFilePath()
	if err != nil {
		fmt.Println("Error getting file path: " + err.Error())
	}

	cfg, err := loadConfig(filePath)

	if err != nil {
		if strings.Contains(err.Error(), "no such file or directory") {
			fmt.Println("No config file found")
			fmt.Println("Getting new user info")
			newClient = true
			getUserName()
			cfg.Username = userName

			address := getServerAddress()
			cfg.ServerAddr = fmt.Sprintf("ws://%s:8080/ws", address)
			saveConfig(cfg)

			privKey, err := generateRSAKeys()
			if err != nil {
				fmt.Println("Error generating keys:", err)
				return
			}

			privKeyPath := filepath.Join(filePath, "private_key.pem")
			pubKeyPath := filepath.Join(filePath, "public_key.pem")

			if err := savePrivateKey(privKeyPath, privKey); err != nil {
				fmt.Println("Error saving private key:", err)
				return
			}

			if err := savePublicKey(pubKeyPath, &privKey.PublicKey); err != nil {
				fmt.Println("Error saving public key:", err)
				return
			}

		} else {
			fmt.Println("There was an error loading the file, but it wasn't that the file can't be found...")
			fmt.Println("Error: " + err.Error())
		}
	} else {
		fmt.Println("Config found!")
		fmt.Println(cfg)
	}

	//load private key
	priv_key, err := loadPrivateKey(filePath)
	if err != nil {
		fmt.Println("Error loading private key: ", err)
		return
	}
	fmt.Println("Private key loaded successfully.")

	//load public key
	pub_key, err := loadPublicKey(filePath)
	if err != nil {
		fmt.Println("Error loading public key: ", err)
		return
	}
	fmt.Println("Public key loaded successfully.")

	conn, err := connect(cfg.ServerAddr)
	if err != nil {
		log.Fatal("Connection failed:", err)
	}

	defer conn.Close()

	fmt.Println("Connected to server at", cfg.ServerAddr)

	done := make(chan struct{})
	go listenServer(conn, done, priv_key)

	sendJoinRequest(conn, pub_key)

	if newClient {
		sendJoinRequest(conn, pub_key)
	}

	go readUserInput(conn)

	// closes connection to server
	waitForInterrupt(conn)

	// Wait for listenServer goroutine to finish
	<-done
}
