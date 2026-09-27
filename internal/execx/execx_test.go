package execx

import (
	"context"
	"strings"
	"testing"
)

func TestOSEnvAndErrors(t *testing.T) {
	r := WithEnv(OS{}, "KONA_TEST_VAR=hello")
	out, err := r.Run(context.Background(), "sh", "-c", "echo $KONA_TEST_VAR")
	if err != nil || strings.TrimSpace(string(out)) != "hello" {
		t.Fatalf("env not passed: %q %v", out, err)
	}
	_, err = OS{}.Run(context.Background(), "sh", "-c", "echo boom >&2; exit 3")
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("stderr not surfaced: %v", err)
	}
	if f := WithEnv(&Fake{}, "X=1"); f == nil {
		t.Fatal("WithEnv dropped fake runner")
	}
}
