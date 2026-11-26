package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	tcpPort = ":9000"
	udpPort = ":9001"
)

type Client struct {
	addr string
	conn net.Conn
	mu   sync.Mutex
}

func (c *Client) send(msg string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err := io.WriteString(c.conn, msg)
	return err
}

type Hub struct {
	mu      sync.RWMutex
	clients map[string]*Client
}

func NewHub() *Hub {
	return &Hub{
		clients: make(map[string]*Client),
	}
}

func (h *Hub) add(c *Client) {
	h.mu.Lock()
	h.clients[c.addr] = c
	h.mu.Unlock()
}

func (h *Hub) remove(addr string) bool {
	h.mu.Lock()
	_, existed := h.clients[addr]
	if existed {
		delete(h.clients, addr)
	}
	h.mu.Unlock()
	return existed
}

func (h *Hub) snapshot(excludeAddr string) []*Client {
	h.mu.RLock()
	list := make([]*Client, 0, len(h.clients))
	for addr, c := range h.clients {
		if addr != excludeAddr {
			list = append(list, c)
		}
	}
	h.mu.RUnlock()
	return list
}

func (h *Hub) broadcast(senderAddr, msg string) {
	targets := h.snapshot(senderAddr)
	for _, c := range targets {
		_ = c.send(msg)
	}
}

func (h *Hub) closeAllAndNotify(finalMsg string) int {
	h.mu.Lock()
	list := make([]*Client, 0, len(h.clients))
	for addr, c := range h.clients {
		list = append(list, c)
		delete(h.clients, addr)
	}
	h.mu.Unlock()

	for _, c := range list {
		_ = c.send(finalMsg)
		_ = c.conn.Close()
	}
	return len(list)
}

func main() {
	fmt.Println("=== Мережевий сервіс диспетчеризації (TCP: 9000, UDP: 9001) ===")

	tcpListener, err := net.Listen("tcp", tcpPort)
	if err != nil {
		log.Fatalf("[ПОМИЛКА] Не вдалося відкрити TCP-слухач %s: %v", tcpPort, err)
	}
	fmt.Printf("[СЕРВЕР] TCP-слухач успішно відкрито на порту %s\n", strings.TrimPrefix(tcpPort, ":"))

	udpConn, err := net.ListenPacket("udp", udpPort)
	if err != nil {
		_ = tcpListener.Close()
		log.Fatalf("[ПОМИЛКА] Не вдалося відкрити UDP-слухач %s: %v", udpPort, err)
	}
	fmt.Printf("[СЕРВЕР] UDP-слухач телеметрії відкрито на порту %s\n", strings.TrimPrefix(udpPort, ":"))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	hub := NewHub()
	var wg sync.WaitGroup

	go func() {
		<-ctx.Done()
		fmt.Println("\n[СЕРВЕР] Отримано сигнал завершення роботи. Запуск Graceful Shutdown...")
		_ = tcpListener.Close()
		_ = udpConn.Close()
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		runUDPTelemetry(ctx, udpConn, hub)
	}()

	for {
		conn, err := tcpListener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			if errors.Is(err, net.ErrClosed) {
				break
			}
			fmt.Printf("[ПОМИЛКА] Помилка Accept: %v\n", err)
			continue
		}

		addr := conn.RemoteAddr().String()
		client := &Client{addr: addr, conn: conn}
		hub.add(client)

		fmt.Printf("[ПІДКЛЮЧЕННЯ] Новий клієнт підключився: %s\n", addr)
		_ = client.send("[СЕРВЕР] Ласкаво просимо до мережевого сервісу диспетчеризації!\n")
		hub.broadcast(addr, fmt.Sprintf("[СИСТЕМА] Підключився новий клієнт: %s\n", addr))

		wg.Add(1)
		go func(c *Client) {
			defer wg.Done()
			handleTCPClient(ctx, c, hub)
		}(client)
	}

	wg.Wait()
	closedCount := hub.closeAllAndNotify("[СИСТЕМА]: SERVER_SHUTTING_DOWN\n")
	fmt.Printf("[СЕРВЕР] Сповіщено активних клієнтів: %d. З'єднання безпечно закриті.\n", closedCount)
	fmt.Println("[СЕРВЕР] Роботу сервера успішно завершено.")
}

func runUDPTelemetry(ctx context.Context, udpConn net.PacketConn, hub *Hub) {
	buf := make([]byte, 4096)
	for {
		_ = udpConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		n, remoteAddr, err := udpConn.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return
			}
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				continue
			}
			fmt.Printf("[ПОМИЛКА] Читання UDP: %v\n", err)
			continue
		}

		payload := strings.TrimSpace(string(buf[:n]))
		fmt.Printf("[UDP ТЕЛЕМЕТРІЯ] Отримано %d байт від %s -> %q\n", n, remoteAddr.String(), payload)

		formatted := fmt.Sprintf("[TELEMETRY from %s]: %s\n", remoteAddr.String(), payload)
		hub.broadcast("", formatted)
	}
}

func handleTCPClient(ctx context.Context, c *Client, hub *Hub) {
	defer func() {
		_ = c.conn.Close()
		if hub.remove(c.addr) {
			hub.broadcast(c.addr, fmt.Sprintf("[СИСТЕМА] Клієнт відключився: %s\n", c.addr))
		}
	}()

	go func() {
		<-ctx.Done()
		_ = c.conn.Close()
	}()

	reader := bufio.NewReader(c.conn)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			if err != io.EOF && ctx.Err() == nil && !errors.Is(err, net.ErrClosed) {
				fmt.Printf("[ПОМИЛКА] Читання від %s: %v\n", c.addr, err)
			}
			return
		}

		cmdLine := strings.TrimSpace(line)
		if cmdLine == "" {
			continue
		}

		upper := strings.ToUpper(cmdLine)
		switch {
		case upper == "TIME":
			now := time.Now().Format(time.RFC3339)
			if err := c.send(fmt.Sprintf("[TIME]: %s\n", now)); err != nil {
				return
			}

		case upper == "CLOSE" || upper == "QUIT":
			_ = c.send("[СЕРВЕР] З'єднання завершується. До зустрічі!\n")
			return

		case strings.HasPrefix(upper, "ECHO"):
			payload := strings.TrimSpace(cmdLine[len("ECHO"):])
			if err := c.send(fmt.Sprintf("[ECHO]: %s\n", payload)); err != nil {
				return
			}

		case strings.HasPrefix(upper, "BROADCAST"):
			payload := strings.TrimSpace(cmdLine[len("BROADCAST"):])
			fmt.Printf("[РОЗСИЛКА] Від %s -> %q\n", c.addr, payload)
			hub.broadcast(c.addr, fmt.Sprintf("[СПОВІЩЕННЯ від %s]: %s\n", c.addr, payload))

		case strings.HasPrefix(upper, "MSG"):
			payload := strings.TrimSpace(cmdLine[len("MSG"):])
			fmt.Printf("[РОЗСИЛКА] Від %s -> %q\n", c.addr, payload)
			hub.broadcast(c.addr, fmt.Sprintf("[СПОВІЩЕННЯ від %s]: %s\n", c.addr, payload))

		default:
			help := "[ПОМИЛКА] Невідома команда. Доступні команди: ECHO <текст>, TIME, BROADCAST <текст> (або MSG <текст>), CLOSE, QUIT\n"
			if err := c.send(help); err != nil {
				return
			}
		}
	}
}
