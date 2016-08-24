package immortal

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"time"
)

type Daemon struct {
	*Config
	*Control
	count      uint64
	lock       uint32
	lock_defer uint32
	process    *process
	sTime      time.Time
}

func (d *Daemon) Run(p Process) {
	if atomic.SwapUint32(&d.lock, uint32(1)) != 0 {
		return
	}

	// increment count by 1
	atomic.AddUint64(&d.count, 1)

	start := time.After(time.Duration(d.Wait) * time.Second)

	var err error
	select {
	case <-start:
		d.process, err = p.Start()
		if err != nil {
			return
		}
		// write parent pid
		if d.Pid.Parent != "" {
			if err := d.WritePid(d.Pid.Parent, os.Getpid()); err != nil {
				log.Println(err)
			}
		}
		// write child pid
		if d.Pid.Child != "" {
			if err := d.WritePid(d.Pid.Child, p.Pid()); err != nil {
				log.Println(err)
			}
		}
		select {
		case err := <-d.process.errch:
			fmt.Printf("d.process.sTime = %+v\n", time.Since(d.process.sTime))
			println(d.process.eTime.Sub(d.process.sTime))
			d.process = nil
			// lock_defer defaults to 0, 1 to run only once/down (don't restart)
			atomic.StoreUint32(&d.lock, d.lock_defer)
			d.Control.done <- err
			return
		default:
		}
	case ctrl := <-d.control:
		fmt.Println("eeeeee")
		switch c := ctrl.(type) {
		case pong:
			c.ch <- "pong\n"
		}
	}
}

type pong struct {
	ch chan string
}

func (d *Daemon) pong() string {
	ch := make(chan string, 1)
	d.control <- pong{ch}
	return <-ch
}

// WritePid write pid to file
func (d *Daemon) WritePid(file string, pid int) error {
	if err := ioutil.WriteFile(file, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		return err
	}
	return nil
}

func New(cfg *Config) (*Daemon, error) {
	var (
		supDir string
		err    error
	)

	if cfg.Cwd != "" {
		supDir = filepath.Join(cfg.Cwd, "supervise")
	} else {
		d, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		supDir = filepath.Join(d, "supervise")
	}

	control := &Control{
		fifo:    make(chan Return),
		quit:    make(chan struct{}),
		done:    make(chan error),
		control: make(chan interface{}),
	}

	// if ctrl create supervise dir
	if cfg.ctrl {
		// create fifo
		var ctrl = []string{"control", "ok"}
		for _, v := range ctrl {
			if err := MakeFifo(filepath.Join(supDir, v)); err != nil {
				return nil, err
			}
		}

		// lock
		if lock, err := os.Create(filepath.Join(supDir, "lock")); err != nil {
			return nil, err
		} else if err = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX+syscall.LOCK_NB); err != nil {
			return nil, err
		}

		// read fifo
		if control.fifo_control, err = OpenFifo(filepath.Join(supDir, "control")); err != nil {
			return nil, err
		}
		if control.fifo_ok, err = OpenFifo(filepath.Join(supDir, "ok")); err != nil {
			return nil, err
		}
	}

	return &Daemon{
		Config:  cfg,
		Control: control,
		sTime:   time.Now(),
	}, nil
}
