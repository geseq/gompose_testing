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

type TestContext struct {
	built   bool
	ip      []byte
	logFile *os.File
	testNum int
}

func RunTest(t *testing.T, c *TestContext, port string, testFunc func([]byte)) {
	if testing.Short() {
		t.Skip("skipping end-to-end test in short mode.")
	}

	// build project if not yet built
	if !c.built {
		if err := exec.Command("./build.sh").Run(); err != nil {
			t.Fatal("build failed: ", err)
		}
		c.built = true
	}

	// get Docker IP and cache it
	if len(c.ip) == 0 {
		dkm, err := exec.Command("docker-machine", "active").Output()
		if err == nil { // active Docker Machine detected, use it
			byteIP, err := exec.Command("docker-machine", "ip", string(bytes.TrimSpace(dkm))).Output()
			if err != nil {
				t.Fatal(err)
			}
			c.ip = bytes.TrimSpace(byteIP)
		} else { // no active docker machine, assume Docker is running natively
			c.ip = []byte("127.0.0.1")
		}
	}

	// bring up Compose
	if err := exec.Command("docker-compose", "up", "-d").Run(); err != nil {
		t.Fatal("Docker compose failed to start: ", err)
	}
	defer func() {
		if err := exec.Command("docker-compose", "down").Run(); err != nil {
			t.Fatal(err)
		}
	}()

	// log Compose output
	// TODO timestamps
	if c.logFile == nil {
		c.logFile, _ = os.Create("test.log")
		cmd := exec.Command("docker-compose", "logs", "--no-color")
		cmd.Stdout = c.logFile
		cmd.Stderr = c.logFile
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
	}
	c.testNum++
	c.logFile.WriteString(fmt.Sprintf("--- test %v start\n", c.testNum))
	defer c.logFile.WriteString(fmt.Sprintf("--- test %v end\n", c.testNum))

	// poll until server is healthy
	start := time.Now()
	for func() bool {
		// TODO don't pass in port. either infer it or use context.
		resp, err := http.Head(fmt.Sprintf("http://%s%v/health_check", c.ip, port))
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
	testFunc(c.ip)
}
