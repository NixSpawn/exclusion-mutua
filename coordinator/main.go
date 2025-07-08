// ===== COORDINADOR CENTRAL =====
// Archivo: coordinator/main.go
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"sort"
	"sync"
	"time"
)

// Tipos de mensajes
type Message struct {
	Type      string `json:"type"`      // REQUEST, REPLY, RELEASE, HEARTBEAT, JOIN, LEAVE
	NodeID    string `json:"node_id"`
	Timestamp int64  `json:"timestamp"`
	Content   string `json:"content"`
}

// Entrada en cola de solicitudes
type QueueEntry struct {
	NodeID    string
	Timestamp int64
	Conn      net.Conn
}

// Cliente conectado
type Client struct {
	ID         string
	Connection net.Conn
	LastSeen   time.Time
	InCritical bool
}

// Coordinador distribuido
type Coordinator struct {
	Clients       map[string]*Client
	RequestQueue  []QueueEntry
	SharedFile    *os.File
	AccessLog     []string
	Mutex         sync.RWMutex
	LogicalClock  int64
	CurrentHolder string
}

// Crear coordinador
func NewCoordinator() *Coordinator {
	// Crear archivo compartido
	file, err := os.Create("shared_resource.txt")
	if err != nil {
		log.Fatal("Error creando archivo compartido:", err)
	}

	return &Coordinator{
		Clients:      make(map[string]*Client),
		RequestQueue: make([]QueueEntry, 0),
		SharedFile:   file,
		AccessLog:    make([]string, 0),
	}
}

// Actualizar reloj lógico
func (c *Coordinator) updateClock(receivedTimestamp int64) int64 {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	
	if receivedTimestamp > c.LogicalClock {
		c.LogicalClock = receivedTimestamp + 1
	} else {
		c.LogicalClock++
	}
	return c.LogicalClock
}

// Agregar cliente
func (c *Coordinator) addClient(id string, conn net.Conn) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	
	c.Clients[id] = &Client{
		ID:         id,
		Connection: conn,
		LastSeen:   time.Now(),
		InCritical: false,
	}
	
	log.Printf("✅ Cliente %s conectado desde %s", id, conn.RemoteAddr())
	c.logAccess(fmt.Sprintf("Cliente %s se unió al sistema", id))
}

// Remover cliente
func (c *Coordinator) removeClient(id string) {
	c.Mutex.Lock()
	defer c.Mutex.Unlock()
	
	if client, exists := c.Clients[id]; exists {
		client.Connection.Close()
		delete(c.Clients, id)
		
		// Remover solicitudes pendientes
		newQueue := make([]QueueEntry, 0)
		for _, entry := range c.RequestQueue {
			if entry.NodeID != id {
				newQueue = append(newQueue, entry)
			}
		}
		c.RequestQueue = newQueue
		
		log.Printf("❌ Cliente %s desconectado", id)
		c.logAccess(fmt.Sprintf("Cliente %s salió del sistema", id))
		
		// Si estaba en sección crítica, liberarla
		if c.CurrentHolder == id {
			c.CurrentHolder = ""
			c.processQueue()
		}
	}
}

// Procesar cola de solicitudes
func (c *Coordinator) processQueue() {
	if len(c.RequestQueue) == 0 || c.CurrentHolder != "" {
		return
	}
	
	// Ordenar cola por timestamp
	sort.Slice(c.RequestQueue, func(i, j int) bool {
		if c.RequestQueue[i].Timestamp == c.RequestQueue[j].Timestamp {
			return c.RequestQueue[i].NodeID < c.RequestQueue[j].NodeID
		}
		return c.RequestQueue[i].Timestamp < c.RequestQueue[j].Timestamp
	})
	
	// Otorgar acceso al primero en cola
	if len(c.RequestQueue) > 0 {
		entry := c.RequestQueue[0]
		c.RequestQueue = c.RequestQueue[1:]
		c.CurrentHolder = entry.NodeID
		
		// Marcar cliente como en sección crítica
		if client, exists := c.Clients[entry.NodeID]; exists {
			client.InCritical = true
		}
		
		// Enviar permiso
		response := Message{
			Type:      "GRANT",
			NodeID:    "COORDINATOR",
			Timestamp: c.updateClock(0),
			Content:   "Acceso otorgado a sección crítica",
		}
		
		c.sendMessage(entry.Conn, response)
		log.Printf("🔒 Acceso otorgado a %s", entry.NodeID)
	}
}

// Manejar mensaje
func (c *Coordinator) handleMessage(msg Message, conn net.Conn) {
	c.updateClock(msg.Timestamp)
	
	switch msg.Type {
	case "JOIN":
		c.addClient(msg.NodeID, conn)
		
	case "REQUEST":
		log.Printf("📨 Solicitud de acceso de %s", msg.NodeID)
		c.Mutex.Lock()
		c.RequestQueue = append(c.RequestQueue, QueueEntry{
			NodeID:    msg.NodeID,
			Timestamp: msg.Timestamp,
			Conn:      conn,
		})
		c.Mutex.Unlock()
		c.processQueue()
		
	case "RELEASE":
		log.Printf("🔓 %s liberó la sección crítica", msg.NodeID)
		c.Mutex.Lock()
		if c.CurrentHolder == msg.NodeID {
			c.CurrentHolder = ""
			if client, exists := c.Clients[msg.NodeID]; exists {
				client.InCritical = false
			}
		}
		c.Mutex.Unlock()
		c.processQueue()
		
	case "HEARTBEAT":
		if client, exists := c.Clients[msg.NodeID]; exists {
			client.LastSeen = time.Now()
		}
		
	case "WRITE":
		if c.CurrentHolder == msg.NodeID {
			c.writeToSharedResource(msg.NodeID, msg.Content)
		}
	}
}

// Escribir al recurso compartido
func (c *Coordinator) writeToSharedResource(nodeID, content string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05.000")
	entry := fmt.Sprintf("[%s] %s: %s\n", timestamp, nodeID, content)
	
	c.SharedFile.WriteString(entry)
	c.SharedFile.Sync()
	
	logEntry := fmt.Sprintf("Nodo %s escribió al recurso en %s", nodeID, timestamp)
	c.logAccess(logEntry)
	
	log.Printf("📝 %s escribió: %s", nodeID, content)
}

// Registrar acceso
func (c *Coordinator) logAccess(entry string) {
	c.AccessLog = append(c.AccessLog, entry)
	if len(c.AccessLog) > 100 { // Limitar tamaño del log
		c.AccessLog = c.AccessLog[1:]
	}
}

// Enviar mensaje
func (c *Coordinator) sendMessage(conn net.Conn, msg Message) {
	data, _ := json.Marshal(msg)
	data = append(data, '\n')
	conn.Write(data)
}

// Manejar conexión de cliente
func (c *Coordinator) handleClient(conn net.Conn) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	
	var clientID string
	
	for scanner.Scan() {
		var msg Message
		if err := json.Unmarshal(scanner.Bytes(), &msg); err != nil {
			log.Printf("Error decodificando mensaje: %v", err)
			continue
		}
		
		if clientID == "" {
			clientID = msg.NodeID
		}
		
		c.handleMessage(msg, conn)
	}
	
	// Cliente desconectado
	if clientID != "" {
		c.removeClient(clientID)
	}
}

// Monitorear clientes inactivos
func (c *Coordinator) monitorClients() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	
	for range ticker.C {
		c.Mutex.RLock()
		for id, client := range c.Clients {
			if time.Since(client.LastSeen) > 10*time.Second {
				log.Printf("⚠️ Cliente %s parece inactivo", id)
				go c.removeClient(id)
			}
		}
		c.Mutex.RUnlock()
	}
}

// Mostrar estado
func (c *Coordinator) showStatus() {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	
	fmt.Printf("\n📊 === ESTADO DEL COORDINADOR ===\n")
	fmt.Printf("Reloj lógico: %d\n", c.LogicalClock)
	fmt.Printf("Clientes conectados: %d\n", len(c.Clients))
	fmt.Printf("Solicitudes en cola: %d\n", len(c.RequestQueue))
	fmt.Printf("Titular actual: %s\n", c.CurrentHolder)
	
	fmt.Println("\n👥 Clientes:")
	for id, client := range c.Clients {
		status := "Libre"
		if client.InCritical {
			status = "En sección crítica"
		}
		fmt.Printf("  - %s: %s (último ping: %s)\n", 
			id, status, client.LastSeen.Format("15:04:05"))
	}
	
	fmt.Println("\n⏳ Cola de solicitudes:")
	for i, entry := range c.RequestQueue {
		fmt.Printf("  %d. %s (timestamp: %d)\n", i+1, entry.NodeID, entry.Timestamp)
	}
}

// Mostrar log de accesos
func (c *Coordinator) showAccessLog() {
	c.Mutex.RLock()
	defer c.Mutex.RUnlock()
	
	fmt.Println("\n📋 === LOG DE ACCESOS ===")
	if len(c.AccessLog) == 0 {
		fmt.Println("(Sin registros)")
	} else {
		for _, entry := range c.AccessLog {
			fmt.Println(entry)
		}
	}
}

// Mostrar contenido del archivo
func (c *Coordinator) showFileContent() {
	fmt.Println("\n📄 === CONTENIDO DEL ARCHIVO COMPARTIDO ===")
	
	data, err := os.ReadFile("shared_resource.txt")
	if err != nil {
		fmt.Printf("Error leyendo archivo: %v\n", err)
		return
	}
	
	if len(data) == 0 {
		fmt.Println("(Archivo vacío)")
	} else {
		fmt.Print(string(data))
	}
}

// Menú interactivo
func (c *Coordinator) runMenu() {
	scanner := bufio.NewScanner(os.Stdin)
	
	fmt.Println("\n🎮 === MENÚ DEL COORDINADOR ===")
	fmt.Println("Comandos disponibles:")
	fmt.Println("  status - Mostrar estado del sistema")
	fmt.Println("  log    - Mostrar log de accesos")
	fmt.Println("  file   - Mostrar contenido del archivo")
	fmt.Println("  quit   - Salir")
	
	for {
		fmt.Print("\nCoordinador> ")
		if !scanner.Scan() {
			break
		}
		
		command := scanner.Text()
		switch command {
		case "status":
			c.showStatus()
		case "log":
			c.showAccessLog()
		case "file":
			c.showFileContent()
		case "quit":
			fmt.Println("👋 Cerrando coordinador...")
			return
		case "":
			// Ignorar entrada vacía
		default:
			fmt.Printf("❌ Comando desconocido: %s\n", command)
		}
	}
}

func main() {
	fmt.Println("🌐 === COORDINADOR DE EXCLUSIÓN MUTUA DISTRIBUIDA ===")
	fmt.Println("Puerto: 8080")
	fmt.Println("================================================")
	
	coordinator := NewCoordinator()
	defer coordinator.SharedFile.Close()
	
	// Iniciar servidor
	listener, err := net.Listen("tcp", ":8080")
	if err != nil {
		log.Fatal("Error iniciando servidor:", err)
	}
	defer listener.Close()
	
	fmt.Println("🚀 Coordinador iniciado en puerto 8080")
	fmt.Println("💡 Ejecute nodos con: go run node/main.go <node_id>")
	
	// Iniciar monitor de clientes
	go coordinator.monitorClients()
	
	// Iniciar menú en goroutine
	go coordinator.runMenu()
	
	// Aceptar conexiones
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Printf("Error aceptando conexión: %v", err)
			continue
		}
		
		go coordinator.handleClient(conn)
	}
}

