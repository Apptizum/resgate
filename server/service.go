package server

import (
	"errors"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
	"github.com/resgateio/resgate/logger"
	"github.com/resgateio/resgate/server/mq"
	"github.com/resgateio/resgate/server/rescache"
)

// Service is a RES gateway implementation
type Service struct {
	cfg      Config
	logger   logger.Logger
	mu       sync.Mutex
	stopping bool
	stop     chan error

	mq    mq.Client
	cache *rescache.Cache

	// httpServer
	h   *http.Server
	enc APIEncoder

	// wsListener/wsConn
	upgrader websocket.Upgrader
	conns    map[string]*wsConn // Connections by wsConn Id's
	wg       sync.WaitGroup     // Wait for all connections to be disconnected
}

// NewService creates a new Service
func NewService(mq mq.Client, cfg Config) (*Service, error) {
	s := &Service{
		cfg: cfg,
		mq:  mq,
	}

	err := s.cfg.prepare()
	if err == nil {
		err = s.initHTTPServer()
	}
	if err == nil {
		err = s.initWSHandler()
	}
	if err == nil {
		err = s.initAPIHandler()
	}
	if err == nil {
		s.initMQClient()
	}

	if err != nil {
		return nil, err
	}
	return s, nil
}

// SetLogger sets the logger
func (s *Service) SetLogger(l logger.Logger) *Service {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop != nil {
		panic("SetLogger must be called before starting server")
	}

	s.logger = l
	s.cache.SetLogger(l)
	return s
}

// Logf writes a formatted log message
func (s *Service) Logf(format string, v ...interface{}) {
	if s.logger == nil {
		return
	}
	s.logger.Logf("[Main] ", format, v...)
}

// Debugf writes a formatted debug message
func (s *Service) Debugf(format string, v ...interface{}) {
	if s.logger == nil {
		return
	}
	s.logger.Debugf("[Main] ", format, v...)
}

// Start connects the Service to the nats server
func (s *Service) Start() (err error) {
	err = s.start()
	if err != nil {
		s.Stop(err)
	}
	return
}

// Start connects the Service to the nats server
func (s *Service) start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.stop != nil {
		return nil
	}
	if s.stopping {
		return errors.New("server is stopping")
	}

	s.Logf("Starting resgate version %s", Version)
	s.stop = make(chan error, 1)

	if err := s.startMQClient(); err != nil {
		s.Logf("Failed to connect to messaging system: %s", err)
		return err
	}

	s.startHTTPServer()

	return nil
}

// Stop closes the connection to the nats server
func (s *Service) Stop(err error) {
	s.mu.Lock()
	if s.stop == nil || s.stopping {
		s.mu.Unlock()
		return
	}
	s.stopping = true
	s.mu.Unlock()

	s.Logf("Stopping server...")

	s.stopWSHandler()
	s.stopHTTPServer()
	s.stopMQClient()

	s.mu.Lock()
	s.stop <- err
	close(s.stop)
	s.stop = nil
	s.stopping = false
	s.Logf("Service stopped")
	s.mu.Unlock()
}

// StopChannel returns a channel that will pass a value
// when the service has stopped.
func (s *Service) StopChannel() <-chan error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stop
}
