// Package mqtt runs an embedded MQTT broker so the server can push commands
// to readers without a separate broker process. Readers authenticate with the
// same API key they use for HTTP and may only subscribe to their own topic.
package mqtt

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	mochi "github.com/mochi-mqtt/server/v2"
	"github.com/mochi-mqtt/server/v2/listeners"
	"github.com/mochi-mqtt/server/v2/packets"

	"github.com/Gestusition/field-day/apps/api/internal/platform/auth"
)

// CommandTopic is where a reader receives command notifications.
func CommandTopic(readerID uuid.UUID) string {
	return "fieldday/readers/" + readerID.String() + "/commands"
}

// Broker wraps the mochi MQTT server.
type Broker struct {
	srv *mochi.Server
	log *slog.Logger
}

// Options configures the broker listener.
type Options struct {
	Addr     string
	CertFile string // optional; enables TLS (port 8883 by convention)
	KeyFile  string
}

// NewBroker creates a broker that authenticates readers with devices.
func NewBroker(opts Options, devices auth.DeviceAuthenticator, log *slog.Logger) (*Broker, error) {
	srv := mochi.New(&mochi.Options{InlineClient: true, Logger: log.With("component", "mqtt")})
	if err := srv.AddHook(&deviceHook{devices: devices, log: log}, nil); err != nil {
		return nil, fmt.Errorf("mqtt: add hook: %w", err)
	}

	cfg := listeners.Config{ID: "tcp", Address: opts.Addr}
	if opts.CertFile != "" {
		cert, err := tls.LoadX509KeyPair(opts.CertFile, opts.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("mqtt: tls: %w", err)
		}
		cfg.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	}
	if err := srv.AddListener(listeners.NewTCP(cfg)); err != nil {
		return nil, fmt.Errorf("mqtt: listener: %w", err)
	}
	return &Broker{srv: srv, log: log}, nil
}

// Start begins accepting connections (non-blocking).
func (b *Broker) Start() error { return b.srv.Serve() }

// Close stops the broker.
func (b *Broker) Close() error { return b.srv.Close() }

// Publish sends payload to topic with QoS 1.
func (b *Broker) Publish(topic string, payload []byte) error {
	return b.srv.Publish(topic, payload, false, 1)
}

// deviceHook authenticates CONNECT packets and restricts subscriptions.
type deviceHook struct {
	mochi.HookBase
	devices auth.DeviceAuthenticator
	log     *slog.Logger
}

func (h *deviceHook) ID() string { return "fieldday-device-auth" }

func (h *deviceHook) Provides(b byte) bool {
	return bytes.Contains([]byte{mochi.OnConnectAuthenticate, mochi.OnACLCheck}, []byte{b})
}

// OnConnectAuthenticate expects username = reader id, password = API key.
func (h *deviceHook) OnConnectAuthenticate(cl *mochi.Client, pk packets.Packet) bool {
	d, err := h.devices.AuthenticateDevice(context.Background(), string(pk.Connect.Password))
	if err != nil {
		h.log.Warn("mqtt connect rejected", "client", cl.ID, "remote", cl.Net.Remote)
		return false
	}
	return d.ReaderID.String() == string(pk.Connect.Username)
}

// OnACLCheck lets a reader subscribe to its own command topic only; readers
// never publish (results go over HTTPS). The inline client is unrestricted.
func (h *deviceHook) OnACLCheck(cl *mochi.Client, topic string, write bool) bool {
	if cl.Net.Inline {
		return true
	}
	id, err := uuid.Parse(string(cl.Properties.Username))
	if err != nil {
		return false
	}
	return !write && topic == CommandTopic(id)
}
