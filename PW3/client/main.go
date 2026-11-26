package main

import (
	"bufio"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"os"
	"strings"
)

const (
	serverAddr     = "127.0.0.1:8443"
	caCertPath     = "certs/ca.crt"
	clientCertPath = "certs/client.crt"
	clientKeyPath  = "certs/client.key"
	alertMessage   = "CRITICAL_ERROR: Database connection timeout on node-04"
)

func main() {
	fmt.Println("=== Агент диспетчеризації (mTLS Client) ===")

	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		log.Fatalf("[ПОМИЛКА] Не вдалося завантажити сертифікат клієнта: %v", err)
	}

	cn := "unknown"
	if len(clientCert.Leaf.Subject.CommonName) > 0 {
		cn = clientCert.Leaf.Subject.CommonName
	} else if len(clientCert.Certificate) > 0 {
		if parsed, err := x509.ParseCertificate(clientCert.Certificate[0]); err == nil {
			cn = parsed.Subject.CommonName
		}
	}
	fmt.Printf("[КЛІЄНТ] Завантажено сертифікат клієнта: %s (CN=%s)\n", clientCertPath, cn)

	caBytes, err := os.ReadFile(caCertPath)
	if err != nil {
		log.Fatalf("[ПОМИЛКА] Не вдалося прочитати %s: %v", caCertPath, err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caBytes) {
		log.Fatalf("[ПОМИЛКА] Не вдалося додати CA у пул")
	}

	tlsCfg := &tls.Config{
		Certificates: []tls.Certificate{clientCert},
		RootCAs:      caPool,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS13,
	}

	fmt.Printf("[КЛІЄНТ] Підключення до шлюзу %s...\n", serverAddr)
	conn, err := tls.Dial("tcp", serverAddr, tlsCfg)
	if err != nil {
		log.Fatalf("[ПОМИЛКА] Не вдалося встановити mTLS-з'єднання: %v", err)
	}
	defer conn.Close()

	state := conn.ConnectionState()
	tlsVer := tls.VersionName(state.Version)
	fmt.Printf("[КЛІЄНТ] mTLS-рукостискання успішне. Використано протокол %s\n", tlsVer)

	fmt.Printf("[КЛІЄНТ] Відправка алерту: %s\n", alertMessage)
	if _, err := fmt.Fprintf(conn, "%s\n", alertMessage); err != nil {
		log.Fatalf("[ПОМИЛКА] Не вдалося надіслати повідомлення: %v", err)
	}

	reply, err := bufio.NewReader(conn).ReadString('\n')
	if err != nil {
		log.Fatalf("[ПОМИЛКА] Не вдалося отримати відповідь від сервера: %v", err)
	}

	fmt.Printf("[КЛІЄНТ] Відповідь від сервера: %s\n", strings.TrimSpace(reply))
	fmt.Println("[КЛІЄНТ] Сесію успішно завершено.")
}
