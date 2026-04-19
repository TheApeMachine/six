package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	. "github.com/smartystreets/goconvey/convey"
)

func TestNewClient(t *testing.T) {
	t.Parallel()

	Convey("Given an unreachable URL", t, func() {
		client := NewClient(context.Background(), "ws://127.0.0.1:1/")

		So(client, ShouldBeNil)
	})

	Convey("Given a running WebSocket echo server", t, func() {
		var upgrader websocket.Upgrader

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			connection, err := upgrader.Upgrade(writer, request, nil)

			if err != nil {
				return
			}

			defer connection.Close()

			for {
				messageType, payload, readErr := connection.ReadMessage()

				if readErr != nil {
					return
				}

				if writeErr := connection.WriteMessage(messageType, payload); writeErr != nil {
					return
				}
			}
		}))

		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
		client := NewClient(context.Background(), wsURL)

		So(client, ShouldNotBeNil)
		So(client.Close(), ShouldBeNil)
	})
}

func TestClient_Write(t *testing.T) {
	t.Parallel()

	Convey("Write sends a binary frame to the server", t, func() {
		var upgrader websocket.Upgrader

		received := make(chan []byte, 1)

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			connection, err := upgrader.Upgrade(writer, request, nil)

			if err != nil {
				return
			}

			defer connection.Close()

			_, payload, readErr := connection.ReadMessage()

			if readErr != nil {
				return
			}

			received <- append([]byte(nil), payload...)
		}))

		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
		client := NewClient(context.Background(), wsURL)

		So(client, ShouldNotBeNil)

		payload := []byte{1, 2, 3, 4}
		n, err := client.Write(payload)

		So(err, ShouldBeNil)
		So(n, ShouldEqual, len(payload))

		var got []byte

		select {
		case got = <-received:
		case <-time.After(2 * time.Second):
			t.Fatal("timed out waiting for WebSocket frame")
		}

		So(got, ShouldResemble, payload)
		So(client.Close(), ShouldBeNil)
	})

	Convey("Write on nil client returns ErrClosedPipe", t, func() {
		var client *Client

		n, err := client.Write([]byte("x"))

		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.ErrClosedPipe)
	})
}

func TestClient_Read(t *testing.T) {
	t.Parallel()

	Convey("Read returns EOF for non-empty buffer on a live client", t, func() {
		var upgrader websocket.Upgrader

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			connection, err := upgrader.Upgrade(writer, request, nil)

			if err != nil {
				return
			}

			defer connection.Close()

			for {
				_, _, readErr := connection.ReadMessage()

				if readErr != nil {
					return
				}
			}
		}))

		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
		client := NewClient(context.Background(), wsURL)

		So(client, ShouldNotBeNil)

		buf := make([]byte, 4)
		n, err := client.Read(buf)

		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.EOF)
		So(client.Close(), ShouldBeNil)
	})

	Convey("Read with empty slice returns zero without EOF", t, func() {
		var upgrader websocket.Upgrader

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			connection, err := upgrader.Upgrade(writer, request, nil)

			if err != nil {
				return
			}

			defer connection.Close()

			for {
				_, _, readErr := connection.ReadMessage()

				if readErr != nil {
					return
				}
			}
		}))

		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
		client := NewClient(context.Background(), wsURL)

		So(client, ShouldNotBeNil)

		n, err := client.Read(nil)

		So(n, ShouldEqual, 0)
		So(err, ShouldBeNil)
		So(client.Close(), ShouldBeNil)
	})

	Convey("Read on nil client returns ErrClosedPipe", t, func() {
		var client *Client

		n, err := client.Read(make([]byte, 1))

		So(n, ShouldEqual, 0)
		So(err, ShouldEqual, io.ErrClosedPipe)
	})
}

func TestClient_Close(t *testing.T) {
	t.Parallel()

	Convey("Close on nil client is safe", t, func() {
		var client *Client

		So(client.Close(), ShouldBeNil)
	})

	Convey("Close releases the connection", t, func() {
		var upgrader websocket.Upgrader

		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			connection, err := upgrader.Upgrade(writer, request, nil)

			if err != nil {
				return
			}

			defer connection.Close()

			for {
				_, _, readErr := connection.ReadMessage()

				if readErr != nil {
					return
				}
			}
		}))

		defer server.Close()

		wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
		client := NewClient(context.Background(), wsURL)

		So(client, ShouldNotBeNil)
		So(client.Close(), ShouldBeNil)
	})
}

func BenchmarkClient_Write(b *testing.B) {
	var upgrader websocket.Upgrader

	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(writer, request, nil)

		if err != nil {
			return
		}

		defer connection.Close()

		for {
			if _, _, readErr := connection.ReadMessage(); readErr != nil {
				return
			}
		}
	}))

	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http")
	client := NewClient(context.Background(), wsURL)

	if client == nil {
		b.Fatal("client is nil")
	}

	defer client.Close()

	payload := []byte{0, 1, 2, 3, 4, 5, 6, 7}

	b.ReportAllocs()
	b.ResetTimer()

	for range b.N {
		if _, err := client.Write(payload); err != nil {
			b.Fatal(err)
		}
	}
}
