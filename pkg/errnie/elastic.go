package errnie

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
	"github.com/elastic/go-elasticsearch/v8/esutil"
)

var (
	esBulkIndexer esutil.BulkIndexer
	esBulkMu      sync.Mutex
)

// defaultBulkFlushBytes is used when BulkFlushBytes is unset or non-positive.
// A small or zero threshold makes esutil treat each log line as “oversize” and
// flush per item; ~5MiB batches amortizes HTTP overhead under high volume.
const defaultBulkFlushBytes = 1

type esLogSink struct {
	bi esutil.BulkIndexer
}

func (s *esLogSink) Write(p []byte) (int, error) {
	if s.bi == nil {
		fmt.Println("errnie: elasticsearch sink is nil; dropping log")
		return len(p), nil
	}
	line := bytes.TrimSpace(p)
	if len(line) == 0 {
		return len(p), nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	raw := sanitizeLogLineForElasticsearch(append([]byte(nil), line...))
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
		fmt.Println(err)
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

/*
ElasticsearchBulkIndexerStats returns live bulk indexer counters when the
Elasticsearch sink is open (logging.elasticsearch.enabled was true and client
startup succeeded). Use this while the process is running to see whether events
are being queued (NumAdded) vs accepted by the cluster (NumIndexed) vs
failures (NumFailed).

When the second return value is false, ES shipping is not active for this process.
*/
func ElasticsearchBulkIndexerStats() (esutil.BulkIndexerStats, bool) {
	esBulkMu.Lock()
	defer esBulkMu.Unlock()
	if esBulkIndexer == nil {
		return esutil.BulkIndexerStats{}, false
	}
	return esBulkIndexer.Stats(), true
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
			fmt.Println(err)
			return nil, fmt.Errorf("read elasticsearch ca_cert: %w", err)
		}
		pool, err := certPoolFromPEM(caPEM)
		if err != nil {
			fmt.Fprintf(os.Stderr, "errnie: warning: failed to parse elasticsearch ca_cert: %v; using empty pool\n", err)
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
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			fmt.Fprintf(os.Stderr, "errnie: warning: failed to load system cert pool: %v; using empty pool\n", err)
			pool = x509.NewCertPool()
		}
		transport = &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				MinVersion: tls.VersionTLS12,
			},
		}
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
		fmt.Println(err)
		return nil, err
	}

	flushBytes := cfg.BulkFlushBytes
	if flushBytes <= 0 {
		flushBytes = defaultBulkFlushBytes
	}
	flushMS := cfg.FlushIntervalMS
	if flushMS <= 0 {
		flushMS = 50
	}
	refresh := strings.TrimSpace(cfg.BulkRefresh)
	if refresh == "" {
		refresh = "false"
	}

	esBulkMu.Lock()
	if esBulkIndexer != nil {
		if err = esBulkIndexer.Close(context.Background()); err != nil {
			fmt.Println(err)
		}

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
		fmt.Println(err)
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
