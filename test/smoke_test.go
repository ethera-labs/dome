package test

import (
	"context"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	setup(context.Background())
	code := m.Run()
	os.Exit(code)
}
