package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	logLimit     = 96 << 10
	stopDeadline = 20 * time.Second
)

var javaVersion = regexp.MustCompile(`(?m)^(?:openjdk|java) version "(?:1\.)?(\d+)`)

func findJava(ctx context.Context, override string, minimum int) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	executable := "java"
	if runtime.GOOS == "windows" {
		executable += ".exe"
	}
	candidates := []string{override}
	if override == "" {
		candidates = []string{executable}
		if home := os.Getenv("JAVA_HOME"); home != "" {
			candidates = append([]string{filepath.Join(home, "bin", executable)}, candidates...)
		}
	}
	var lastErr error
	for _, candidate := range candidates {
		path, err := exec.LookPath(candidate)
		if err != nil {
			lastErr = err
			continue
		}
		path, err = filepath.Abs(path)
		if err != nil {
			lastErr = err
			continue
		}
		output, err := exec.CommandContext(ctx, path, "-version").CombinedOutput()
		if err != nil {
			lastErr = err
			continue
		}
		match := javaVersion.FindSubmatch(output)
		if len(match) != 2 {
			lastErr = errors.New(tr("не удалось определить версию Java"))
			continue
		}
		major, err := strconv.Atoi(string(match[1]))
		if err != nil {
			lastErr = err
			continue
		}
		if major == minimum || minimum >= 21 && major > minimum {
			return path, nil
		}
		lastErr = fmt.Errorf(tr("найдена Java %d"), major)
	}
	return "", fmt.Errorf(tr("требуется Java %d, укажите JAVA_HOME или -java (%w)"), minimum, lastErr)
}

type consoleLog struct {
	mu      sync.Mutex
	data    []byte
	version uint64
}

func (l *consoleLog) Write(data []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.data = append(l.data, data...)
	if extra := len(l.data) - logLimit; extra > 0 {
		copy(l.data, l.data[extra:])
		l.data = l.data[:logLimit]
	}
	l.version++
	return len(data), nil
}

func (l *consoleLog) snapshot(previous uint64) (string, uint64) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if previous == l.version {
		return "", previous
	}
	return string(l.data), l.version
}

type serverProcess struct {
	cmd      *exec.Cmd
	log      *consoleLog
	input    chan string
	stopping chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	err      error
}

func (p profile) minRAM() int {
	if p.MinMemory > 0 {
		return p.MinMemory
	}
	return 256
}
func launchServer(p profile, java string) (*serverProcess, error) {
	log := &consoleLog{}
	launch, err := serverArgs(p)
	if err != nil {
		return nil, err
	}
	args := []string{fmt.Sprintf("-Xms%dM", p.minRAM()), fmt.Sprintf("-Xmx%dM", p.Memory)}
	for _, arg := range strings.Split(p.JVMArgs, "\n") {
		if arg = strings.TrimSpace(arg); arg != "" {
			args = append(args, arg)
		}
	}
	cmd := exec.Command(java, append(args, launch...)...)
	cmd.Dir = p.dir
	cmd.Stdout, cmd.Stderr = log, log
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	process := &serverProcess{
		cmd: cmd, log: log, input: make(chan string, 16),
		stopping: make(chan struct{}), done: make(chan struct{}),
	}
	go process.forwardInput(stdin)
	go func() {
		process.err = cmd.Wait()
		close(process.done)
	}()
	return process, nil
}

func (p *serverProcess) forwardInput(stdin io.WriteCloser) {
	defer stdin.Close()
	for {
		// Reserve the stop path even when the operator's command queue is full.
		select {
		case <-p.stopping:
			fmt.Fprintln(stdin, "stop")
			return
		default:
		}
		select {
		case <-p.done:
			return
		case <-p.stopping:
			fmt.Fprintln(stdin, "stop")
			return
		case command := <-p.input:
			if _, err := fmt.Fprintln(stdin, command); err != nil {
				return
			}
		}
	}
}

func (p *serverProcess) send(command string) error {
	if strings.ContainsAny(command, "\r\n") {
		return errors.New(tr("введите одну команду сервера"))
	}
	select {
	case <-p.done:
		return errors.New(tr("сервер остановлен"))
	case <-p.stopping:
		return errors.New(tr("сервер останавливается"))
	default:
	}
	select {
	case <-p.stopping:
		return errors.New(tr("сервер останавливается"))
	case p.input <- command:
		return nil
	default:
		return errors.New(tr("очередь команд сервера заполнена"))
	}
}

func (p *serverProcess) stop() {
	p.stopOnce.Do(func() {
		close(p.stopping)
		go func() {
			timer := time.NewTimer(stopDeadline)
			defer timer.Stop()
			select {
			case <-p.done:
			case <-timer.C:
				_ = p.cmd.Process.Kill()
			}
		}()
	})
}
