package errnie

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esapi"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

var (
	esBulkIndexer esutil.BulkIndexer
	esBulkMu      sync.Mutex
)

type esLogSink struct {
	bi esutil.BulkIndexer
}

func (s *esLogSink) Write(p []byte) (int, error) {
	if s.bi == nil {
		return len(p), nil
	}
	line := bytes.TrimSpace(p)
	if len(line) == 0 {
		return len(p), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw := append([]byte(nil), line...)
	err := s.bi.Add(ctx, esutil.BulkIndexerItem{
		Action: "index",
		Body:   bytes.NewReader(raw),
		OnFailure: func(_ context.Context, _ esutil.BulkIndexerItem, resp esutil.BulkIndexerResponseItem, err error) {
			reason := resp.Error.Reason
			if reason == "" && err != nil {
				reason = err.Error()
			}
			fmt.Fprintf(os.Stderr, "errnie: elasticsearch index item failed status=%d type=%s reason=%q\n",
				resp.Status, resp.Error.Type, reason)
		},
	})
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (s *esLogSink) Sync() error { return nil }

func closeElasticsearchSink(ctx context.Context) {
	esBulkMu.Lock()
	defer esBulkMu.Unlock()
	if esBulkIndexer != nil {
		_ = esBulkIndexer.Close(ctx)
		esBulkIndexer = nil
	}
}

func newElasticsearchClientAndSink(cfg ElasticsearchConfig) (io.Writer, error) {
	if !cfg.Enabled {
		closeElasticsearchSink(context.Background())
		return nil, nil
	}

	addrs := make([]string, 0, len(cfg.URLs))
	for _, u := range cfg.URLs {
		u = strings.TrimSpace(u)
		if u != "" {
			addrs = append(addrs, u)
		}
	}
	if len(addrs) == 0 {
		addrs = []string{"https://127.0.0.1:9200"}
	}

	user := strings.TrimSpace(cfg.Username)
	if user == "" {
		user = "elastic"
	}
	pass := strings.TrimSpace(cfg.Password)
	apiKey := strings.TrimSpace(cfg.APIKey)

	caPath := strings.TrimSpace(cfg.CACertPath)
	var transport http.RoundTripper
	if caPath != "" {
		caPEM, err := os.ReadFile(caPath)
		if err != nil {
			return nil, fmt.Errorf("read elasticsearch ca_cert: %w", err)
		}
		pool, err := certPoolFromPEM(caPEM)
		if err != nil {
			return nil, err
		}
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		}
	} else if cfg.InsecureSkipVerify {
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: true,
				MinVersion:         tls.VersionTLS12,
			},
		}
	} else {
		return nil, errors.New("elasticsearch: set logging.elasticsearch.ca_cert to the cluster CA PEM, or insecure_skip_verify: true for local dev only")
	}

	index := strings.TrimSpace(cfg.Index)
	if index == "" {
		index = "six-logs"
	}

	escfg := elasticsearch.Config{
		Addresses: addrs,
		Transport: transport,
	}
	if apiKey != "" {
		escfg.APIKey = apiKey
	} else {
		escfg.Username = user
		escfg.Password = pass
	}

	client, err := elasticsearch.NewClient(escfg)
	if err != nil {
		return nil, err
	}

	ctxPing, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var infoReq esapi.InfoRequest
	infoRes, err := infoReq.Do(ctxPing, client)
	if err != nil {
		return nil, fmt.Errorf("elasticsearch cluster info: %w", err)
	}
	body, _ := io.ReadAll(infoRes.Body)
	_ = infoRes.Body.Close()
	if infoRes.IsError() {
		return nil, fmt.Errorf("elasticsearch cluster info: %s: %s", infoRes.Status(), strings.TrimSpace(string(body)))
	}

	flushBytes := cfg.BulkFlushBytes
	if flushBytes <= 0 {
		// Any positive value ≤ typical doc size triggers per-item flush in esutil (oversize path).
		flushBytes = 1
	}
	flushMS := cfg.FlushIntervalMS
	if flushMS <= 0 {
		flushMS = 50
	}
	refresh := strings.TrimSpace(cfg.BulkRefresh)
	if refresh == "" {
		refresh = "true"
	}

	esBulkMu.Lock()
	if esBulkIndexer != nil {
		_ = esBulkIndexer.Close(context.Background())
		esBulkIndexer = nil
	}
	bi, err := esutil.NewBulkIndexer(esutil.BulkIndexerConfig{
		Client:        client,
		Index:         index,
		NumWorkers:    1,
		FlushBytes:    flushBytes,
		FlushInterval: time.Duration(flushMS) * time.Millisecond,
		Refresh:       refresh,
		OnError: func(_ context.Context, err error) {
			fmt.Fprintf(os.Stderr, "errnie: elasticsearch bulk error: %v\n", err)
		},
	})
	if err != nil {
		esBulkMu.Unlock()
		return nil, err
	}
	esBulkIndexer = bi
	esBulkMu.Unlock()

	return &esLogSink{bi: bi}, nil
}

func certPoolFromPEM(pem []byte) (*x509.CertPool, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("no certificates parsed from PEM")
	}
	return pool, nil
}
