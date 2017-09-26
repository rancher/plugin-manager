package events

import (
	"reflect"
	"time"

	log "github.com/Sirupsen/logrus"
	"github.com/fsouza/go-dockerclient"
)

const workerTimeout = 60 * time.Second

type Handler interface {
	Handle(*docker.APIEvents) error
}

type EventRouter struct {
	handlers      map[string][]Handler
	dockerClient  *docker.Client
	listener      chan *docker.APIEvents
	workers       chan *worker
	workerTimeout time.Duration
}

func NewEventRouter(bufferSize int, workerPoolSize int, dockerClient *docker.Client,
	handlers map[string][]Handler) (*EventRouter, error) {
	workers := make(chan *worker, workerPoolSize)
	for i := 0; i < workerPoolSize; i++ {
		workers <- &worker{}
	}

	eventRouter := &EventRouter{
		handlers:      handlers,
		dockerClient:  dockerClient,
		listener:      make(chan *docker.APIEvents, bufferSize),
		workers:       workers,
		workerTimeout: workerTimeout,
	}

	return eventRouter, nil
}

func (e *EventRouter) Start() error {
	log.Info("Starting event router.")
	go e.routeEvents()
	return e.dockerClient.AddEventListener(e.listener)
}

func (e *EventRouter) Stop() error {
	if e.listener == nil {
		return nil
	}
	return e.dockerClient.RemoveEventListener(e.listener)
}

func (e *EventRouter) routeEvents() {
	for {
		event := <-e.listener
		timer := time.NewTimer(e.workerTimeout)
		gotWorker := false
		for !gotWorker {
			select {
			case w := <-e.workers:
				go w.doWork(event, e)
				gotWorker = true
			case <-timer.C:
				log.Infof("Timed out waiting for worker. Re-initializing wait.")
			}
		}
	}
}

type worker struct{}

func (w *worker) doWork(event *docker.APIEvents, e *EventRouter) {
	defer func() { e.workers <- w }()
	if event == nil {
		return
	}
	if handlers, ok := e.handlers[event.Status]; ok {
		log.Debugf("Processing event: %#v", event)
		for _, handler := range handlers {
			if reflect.ValueOf(handler).IsNil() {
				continue
			}
			if err := handler.Handle(event); err != nil {
				log.Errorf("Error processing event %#v. Error: %v", event, err)
			}
		}
	}
}
