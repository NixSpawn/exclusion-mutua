// ===== NODO CLIENTE =====
// Archivo: node/main.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Mensaje para comunicación
type Message struct {
	Type      string `json:"type"`
	NodeID    string `json:"node_id"`
	Timestamp int64  `json:"timestamp"`
	Content   string `json:"content"`
}

// Nodo cliente
type Node struct {
	ID              string
	LogicalClock    int64
	Connection      net.Conn
	Mutex           sync.Mutex
	InCritical      bool
	RequestPending  bool
	ConnectedToCord bool
}

// Crear nodo
func NewNode(id string) *Node {
	return &Node{
		ID:           id,
		LogicalClock: 0,
	}
}

// Actualizar reloj lógico
func (n *Node) updateClock(receivedTimestamp int64) int64 {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	if receivedTimestamp > n.LogicalClock {
		n.LogicalClock = receivedTimestamp + 1
	} else {
		n.LogicalClock++
	}
	return n.LogicalClock
}

// Conectar al coordinador
func (n *Node) connectToCoordinator() error {
	conn, err := net.Dial("tcp", "localhost:8080")
	if err != nil {
		return err
	}

	n.Connection = conn
	n.ConnectedToCord = true

	// Enviar mensaje de JOIN
	joinMsg := Message{
		Type:      "JOIN",
		NodeID:    n.ID,
		Timestamp: n.updateClock(0),
		Content:   "Nodo conectándose",
	}

	n.sendMessage(joinMsg)
	fmt.Printf("✅ Nodo %s conectado al coordinador\n", n.ID)

	return nil
}

// Enviar mensaje
func (n *Node) sendMessage(msg Message) {
	if n.Connection == nil {
		log.Println("❌ No hay conexión con el coordinador")
		return
	}

	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	n.Connection.Write(data)
}

// Solicitar acceso a sección crítica
func (n *Node) requestAccess() {
	n.Mutex.Lock()
	if n.RequestPending || n.InCritical {
		n.Mutex.Unlock()
		fmt.Println("⚠️ Ya hay una solicitud pendiente o estás en sección crítica")
		return
	}
	n.RequestPending = true
	n.Mutex.Unlock()

	msg := Message{
		Type:      "REQUEST",
		NodeID:    n.ID,
		Timestamp: n.updateClock(0),
		Content:   "Solicitando acceso a sección crítica",
	}

	n.sendMessage(msg)
	fmt.Printf("🔄 Nodo %s: Solicitando acceso a sección crítica\n", n.ID)
}

// Liberar sección crítica
func (n *Node) releaseAccess() {
	n.Mutex.Lock()
	if !n.InCritical {
		n.Mutex.Unlock()
		fmt.Println("⚠️ No estás en sección crítica")
		return
	}
	n.InCritical = false
	n.Mutex.Unlock()

	msg := Message{
		Type:      "RELEASE",
		NodeID:    n.ID,
		Timestamp: n.updateClock(0),
		Content:   "Liberando sección crítica",
	}

	n.sendMessage(msg)
	fmt.Printf("🔓 Nodo %s: Liberando sección crítica\n", n.ID)
}

// Escribir al recurso compartido
func (n *Node) writeToResource(content string) {
	n.Mutex.Lock()
	if !n.InCritical {
		n.Mutex.Unlock()
		fmt.Println("⚠️ No estás en sección crítica")
		return
	}
	n.Mutex.Unlock()

	msg := Message{
		Type:      "WRITE",
		NodeID:    n.ID,
		Timestamp: n.updateClock(0),
		Content:   content,
	}

	n.sendMessage(msg)
	fmt.Printf("📝 Nodo %s: Escribiendo '%s' al recurso\n", n.ID, content)
}

// Escuchar mensajes del coordinador
func (n *Node) listenToCoordinator() {
	scanner := bufio.NewScanner(n.Connection)

	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Error decodificando mensaje: %v", err)
			continue
		}

		n.updateClock(msg.Timestamp)

		switch msg.Type {
		case "GRANT":
			n.Mutex.Lock()
			n.InCritical = true
			n.RequestPending = false
			n.Mutex.Unlock()

			fmt.Printf("🔒 Nodo %s: ACCESO OTORGADO - Ahora en sección crítica\n", n.ID)
			fmt.Println("💡 Usa 'write <mensaje>' para escribir al recurso")
			fmt.Println("💡 Usa 'release' para liberar la sección crítica")
		}
	}

	// Conexión perdida
	n.ConnectedToCord = false
	fmt.Println("❌ Conexión con coordinador perdida")
}

// Enviar heartbeat
func (n *Node) sendHeartbeat() {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if !n.ConnectedToCord {
			return
		}

		msg := Message{
			Type:      "HEARTBEAT",
			NodeID:    n.ID,
			Timestamp: n.updateClock(0),
			Content:   "ping",
		}

		n.sendMessage(msg)
	}
}

// Mostrar estado
func (n *Node) showStatus() {
	n.Mutex.Lock()
	defer n.Mutex.Unlock()

	fmt.Printf("\n📊 === ESTADO DEL NODO %s ===\n", n.ID)
	fmt.Printf("Reloj lógico: %d\n", n.LogicalClock)
	fmt.Printf("Conectado: %v\n", n.ConnectedToCord)
	fmt.Printf("En sección crítica: %v\n", n.InCritical)
	fmt.Printf("Solicitud pendiente: %v\n", n.RequestPending)
}

// Menú interactivo
func (n *Node) runMenu() {
	scanner := bufio.NewScanner(os.Stdin)

	fmt.Printf("\n🎮 === MENÚ DEL NODO %s ===\n", n.ID)
	fmt.Println("Comandos disponibles:")
	fmt.Println("  request        - Solicitar acceso a sección crítica")
	fmt.Println("  write <texto>  - Escribir al recurso (solo en sección crítica)")
	fmt.Println("  release        - Liberar sección crítica")
	fmt.Println("  status         - Mostrar estado del nodo")
	fmt.Println("  quit           - Salir")

	for {
		fmt.Printf("\nNodo %s> ", n.ID)
		if !scanner.Scan() {
			break
		}

		input := scanner.Text()
		if input == "" {
			continue
		}

		parts := bufio.NewScanner(strings.NewReader(input))
		parts.Split(bufio.ScanWords)

		var command string
		var args []string

		if parts.Scan() {
			command = parts.Text()
		}

		for parts.Scan() {
			args = append(args, parts.Text())
		}

		switch command {
		case "request":
			n.requestAccess()
		case "write":
			if len(args) == 0 {
				fmt.Println("❌ Uso: write <mensaje>")
				continue
			}
			content := strings.Join(args, " ")
			n.writeToResource(content)
		case "release":
			n.releaseAccess()
		case "status":
			n.showStatus()
		case "quit":
			fmt.Printf("👋 Nodo %s desconectándose...\n", n.ID)
			return
		default:
			fmt.Printf("❌ Comando desconocido: %s\n", command)
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("❌ Uso: go run node/main.go <node_id>")
		fmt.Println("Ejemplo: go run node/main.go Node1")
		os.Exit(1)
	}

	nodeID := os.Args[1]

	fmt.Printf("🚀 === NODO %s ===\n", nodeID)
	fmt.Println("Conectando al coordinador...")

	node := NewNode(nodeID)

	// Conectar al coordinador
	if err := node.connectToCoordinator(); err != nil {
		log.Fatal("Error conectando al coordinador:", err)
	}
	defer node.Connection.Close()

	// Iniciar listener de mensajes
	go node.listenToCoordinator()

	// Iniciar heartbeat
	go node.sendHeartbeat()

	// Esperar un momento para establecer conexión
	time.Sleep(500 * time.Millisecond)

	// Ejecutar menú interactivo
	node.runMenu()
}
