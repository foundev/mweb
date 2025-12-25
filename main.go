package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	defaultPort         = 8080
	defaultReadTimeout  = 5 * time.Second
	defaultWriteTimeout = 10 * time.Second
	shutdownTimeout     = 10 * time.Second
	defaultRootMessage  = "mweb is running\n"
	defaultHealthBody   = "{\"status\":\"ok\"}\n"
)

func main() {
	logger := log.New(os.Stdout, "mweb ", log.LstdFlags)
	configPath := flag.String("config", "", "path to JSON config file")
	flag.Parse()

	config, configErr := loadConfig(*configPath)
	if configErr != nil {
		logger.Fatalf("config error: %v", configErr)
	}

	port := portFromEnv(logger, config.Port)

	mux := http.NewServeMux()
	hostIndex := indexHosts(config)
	mux.HandleFunc("/", rootHandler(hostIndex, config))
	mux.HandleFunc("/healthz", healthHandler(hostIndex, config))

	server := &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: defaultReadTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       60 * time.Second,
	}

	shutdownCh := make(chan os.Signal, 1)
	signal.Notify(shutdownCh, os.Interrupt, syscall.SIGTERM)

	go func() {
		<-shutdownCh
		logger.Println("shutdown signal received")
		ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			logger.Printf("shutdown error: %v", err)
		}
	}()

	logger.Printf("listening on http://localhost:%d", port)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logger.Fatalf("server error: %v", err)
	}
}

func portFromEnv(logger *log.Logger, configPort int) int {
	if configPort > 0 {
		return configPort
	}

	value := os.Getenv("PORT")
	if value == "" {
		return defaultPort
	}

	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		logger.Printf("invalid PORT %q, using default %d", value, defaultPort)
		return defaultPort
	}

	return port
}

type Config struct {
	Port        int          `json:"port"`
	DefaultHost string       `json:"default_host"`
	Hosts       []HostConfig `json:"hosts"`
}

type HostConfig struct {
	Name          string `json:"name"`
	RootMessage   string `json:"root_message"`
	HealthMessage string `json:"health_message"`
	Directory     string `json:"directory"`
}

func loadConfig(path string) (Config, error) {
	if path == "" {
		return Config{}, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return Config{}, fmt.Errorf("open config: %w", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return Config{}, fmt.Errorf("read config: %w", err)
	}

	var config Config
	if err := json.Unmarshal(data, &config); err != nil {
		return Config{}, fmt.Errorf("parse config: %w", err)
	}

	if config.Port < 0 || config.Port > 65535 {
		return Config{}, fmt.Errorf("invalid port %d in config", config.Port)
	}

	for _, host := range config.Hosts {
		if strings.TrimSpace(host.Name) == "" {
			return Config{}, fmt.Errorf("host name cannot be empty")
		}

		if host.Directory != "" {
			info, err := os.Stat(host.Directory)
			if err != nil {
				return Config{}, fmt.Errorf("host %q directory error: %w", host.Name, err)
			}
			if !info.IsDir() {
				return Config{}, fmt.Errorf("host %q directory %q is not a directory", host.Name, host.Directory)
			}
		}
	}

	return config, nil
}

func indexHosts(config Config) map[string]HostConfig {
	hosts := make(map[string]HostConfig, len(config.Hosts))
	for _, host := range config.Hosts {
		hosts[strings.ToLower(host.Name)] = host
	}
	return hosts
}

func rootHandler(hosts map[string]HostConfig, config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, ok := hostForRequest(r, hosts, config)
		if !ok {
			http.NotFound(w, r)
			return
		}

		if host.Directory != "" {
			http.FileServer(http.Dir(host.Directory)).ServeHTTP(w, r)
			return
		}

		message := defaultRootMessage
		if host.RootMessage != "" {
			message = host.RootMessage
			if !strings.HasSuffix(message, "\n") {
				message += "\n"
			}
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(message))
	}
}

func healthHandler(hosts map[string]HostConfig, config Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		host, ok := hostForRequest(r, hosts, config)
		if !ok {
			http.NotFound(w, r)
			return
		}

		body := defaultHealthBody
		if host.HealthMessage != "" {
			body = host.HealthMessage
			if !strings.HasSuffix(body, "\n") {
				body += "\n"
			}
		}

		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}
}

func hostForRequest(r *http.Request, hosts map[string]HostConfig, config Config) (HostConfig, bool) {
	if len(hosts) == 0 {
		return HostConfig{}, true
	}

	requestHost := strings.ToLower(r.Host)
	if requestHost == "" {
		return HostConfig{}, false
	}

	requestHost = strings.Split(requestHost, ":")[0]
	if host, ok := hosts[requestHost]; ok {
		return host, true
	}

	defaultHost := strings.ToLower(config.DefaultHost)
	if defaultHost == "" && len(config.Hosts) == 1 {
		defaultHost = strings.ToLower(config.Hosts[0].Name)
	}
	if defaultHost != "" {
		if host, ok := hosts[defaultHost]; ok {
			return host, true
		}
	}

	return HostConfig{}, false
}
