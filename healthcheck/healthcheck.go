// Package healthcheck contains a monitor which takes a set of liveliness checks
// which it periodically checks. If a check fails after its configured number
// of allowed call attempts, the monitor will send a request to shutdown using
// the function is is provided in its config. Checks are dispatched in their own
// goroutines so that they do not block each other.
package healthcheck

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lightningnetwork/lnd/ticker"
)

// Config contains configuration settings for our monitor.
type Config struct {
	// Checks is a set of health checks that assert that lnd has access to
	// critical resources.
	Checks []*Observation

	// Shutdown should be called to request safe shutdown on failure of a
	// health check.
	Shutdown shutdownFunc
}

// shutdownFunc is the signature we use for a shutdown function which allows us
// to print our reason for shutdown.
type shutdownFunc func(format string, params ...interface{})

// Monitor periodically checks a series of configured liveliness checks to
// ensure that lnd has access to all critical resources.
type Monitor struct {
	started int32 // To be used atomically.
	stopped int32 // To be used atomically.

	cfg *Config

	quit chan struct{}
	wg   sync.WaitGroup
}

// NewMonitor returns a monitor with the provided config.
func NewMonitor(cfg *Config) *Monitor {
	return &Monitor{
		cfg:  cfg,
		quit: make(chan struct{}),
	}
}

// Start launches the goroutines required to run our monitor.
func (m *Monitor) Start() error {
	if !atomic.CompareAndSwapInt32(&m.started, 0, 1) {
		return errors.New("monitor already started")
	}

	// Run through all of the health checks that we have configured and
	// start a goroutine for each check.
	for _, check := range m.cfg.Checks {
		check := check

		// Skip over health checks that are disabled by setting zero
		// attempts.
		if check.Attempts == 0 {
			log.Warnf("check: %v configured with 0 attempts, "+
				"skipping it", check.Name)

			continue
		}

		m.wg.Add(1)
		go func() {
			defer m.wg.Done()
			check.monitor(m.cfg.Shutdown, m.quit)
		}()
	}

	return nil
}

// Stop sends all goroutines the signal to exit and waits for them to exit.
func (m *Monitor) Stop() error {
	if !atomic.CompareAndSwapInt32(&m.stopped, 0, 1) {
		return fmt.Errorf("monitor already stopped")
	}

	close(m.quit)
	m.wg.Wait()

	return nil
}

// Observation represents a liveliness check that we periodically check.
type Observation struct {
	// Name describes the health check.
	Name string

	// Check runs the health check itself, returning an error on failure.
	Check func() error

	// Interval is a ticker which triggers running our check function. This
	// ticker must be started and stopped by the observation.
	Interval ticker.Ticker

	// Attempts is the number of calls we make for a single check before
	// failing.
	Attempts int

	// Timeout is the amount of time we allow our check function to take
	// before we time it out.
	Timeout time.Duration

	// Backoff is the amount of time we back off between retries for failed
	// checks.
	Backoff time.Duration
}

// NewObservation creates an observation.
func NewObservation(name string, check func() error, interval,
	timeout, backoff time.Duration, attempts int) *Observation {

	return &Observation{
		Name:     name,
		Check:    check,
		Interval: ticker.New(interval),
		Attempts: attempts,
		Timeout:  timeout,
		Backoff:  backoff,
	}
}

// String returns a string representation of an observation.
func (o *Observation) String() string {
	return o.Name
}

// monitor executes a health check every time its interval ticks until the quit
// channel signals that we should shutdown. This function is also responsible
// for starting and stopping our ticker.
func (o *Observation) monitor(shutdown shutdownFunc, quit chan struct{}) {
	log.Debugf("Monitoring: %v", o)

	o.Interval.Resume()
	defer o.Interval.Stop()

	for {
		select {
		case <-o.Interval.Ticks():
			o.retryCheck(quit, shutdown)

		// Exit if we receive the instruction to shutdown.
		case <-quit:
			return
		}
	}
}

// retryCheck calls a check function until it succeeds, or we reach our
// configured number of attempts, waiting for our back off period between failed
// calls. If we fail to obtain a passing health check after the allowed number
// of calls, we will request shutdown.
func (o *Observation) retryCheck(quit chan struct{}, shutdown shutdownFunc) {
	var count int

	for count < o.Attempts {
		// Increment our call count and call the health check endpoint.
		count++

		// Create a buffered channel that we will send our health check
		// or timeout error on.
		errChan := make(chan error, 1)
		go func() {
			errChan <- o.Check()
		}()

		// Wait for our check to return, timeout to elapse, or quit
		// signal to be received.
		var err error
		select {
		case err = <-errChan:

		case <-time.After(o.Timeout):
			err = errors.New("health check timed out")

		case <-quit:
			return
		}

		// If our error is nil, we have passed our health check, so we
		// can exit.
		if err == nil {
			return
		}

		// If we have reached our allowed number of attempts, this
		// check has failed so we request shutdown.
		if count == o.Attempts {
			shutdown("Health check: %v failed after %v "+
				"calls", o, o.Attempts)

			return
		}

		// If we are still within the number of calls allowed for this
		// check, we wait for our back off period to elapse, or exit if
		// we get the signal to shutdown.
		select {
		case <-time.After(o.Backoff):
			log.Debugf("Health check: %v, call: %v failed "+
				"with: %v, backing off for: %v", o, count, err,
				o.Timeout)

		case <-quit:
			return
		}
	}
}
