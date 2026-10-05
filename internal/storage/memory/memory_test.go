package memory

import (
	"testing"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/storage/storagetest"
)

func TestRepository(t *testing.T) {
	storagetest.Run(t, func(*testing.T) booking.Repository { return New() })
}
