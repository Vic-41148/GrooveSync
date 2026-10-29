//go:build contract

package navidrome

import (
	"testing"

	_ "github.com/testcontainers/testcontainers-go"
	_ "github.com/testcontainers/testcontainers-go/wait"
)

func TestContractBuildTag(t *testing.T) {
	t.Parallel()
}
