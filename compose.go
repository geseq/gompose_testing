package compose

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"
)

type testContext struct {
	built   bool
	ip      []byte
	logFile *os.File
	testNum int
}

var context testContext

// RunTest executes the passed function using Compose
func RunTest(t *testing.T, port string, testFunc func([]byte)) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode.")
	}

	// build project if not yet built
	if !context.built {
		if err := exec.Command("./build.sh").Run(); err != nil {
			t.Fatal("build failed: ", err)
		}
		context.built = true
	}

	// get Docker IP and cache it
	if len(context.ip) == 0 {
		dkm, err := exec.Command("docker-machine", "active").Output()
		if err == nil { // active Docker Machine detected, use it
			byteIP, err := exec.Command("docker-machine", "ip", string(bytes.TrimSpace(dkm))).Output()
			if err != nil {
				t.Fatal(err)
			}
			context.ip = bytes.TrimSpace(byteIP)
		} else { // no active docker machine, assume Docker is running natively
			context.ip = []byte("127.0.0.1")
		}
	}

	// bring up Compose
	if out, err := exec.Command("docker-compose", "up", "-d").CombinedOutput(); err != nil {
		t.Fatalf("Docker Compose failed to start: %s\n", out)
	}
	defer func() {
		if err := exec.Command("docker-compose", "down").Run(); err != nil {
			// could be Compose version 1.5.x or earlier. fallback to other commands.
			if out, err := exec.Command("docker-compose", "stop").CombinedOutput(); err != nil {
				t.Fatalf("Docker Compose failed to stop: %s\n", out)
			}
			if out, err := exec.Command("docker-compose", "rm", "-f").CombinedOutput(); err != nil {
				t.Fatalf("Docker Compose failed to remove containers: %s\n", out)
			}
		}
	}()

	// log Compose output
	// TODO timestamps
	if context.logFile == nil {
		context.logFile, _ = os.Create("test.log")
	}
	cmd := exec.Command("docker-compose", "logs", "--no-color")
	cmd.Stdout = context.logFile
	cmd.Stderr = context.logFile
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	context.testNum++
	context.logFile.WriteString(fmt.Sprintf("--- test %v start\n", context.testNum))
	defer context.logFile.WriteString(fmt.Sprintf("--- test %v end\n", context.testNum))

	// poll until server is healthy
	start := time.Now()
	for func() bool {
		// TODO don't pass in port. either infer it or use context.
		resp, err := http.Head(fmt.Sprintf("http://%s%v/health_check", context.ip, port))
		if err == nil && resp.StatusCode == 204 {
			return false
		}
		return true
	}() {
		if time.Now().Sub(start) > time.Second*30 {
			t.Fatal("timed out waiting for server to start.")
		}
		time.Sleep(time.Millisecond * 250)
	}

	// Run test
	testFunc(context.ip)
}
