package searchingester

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSearchIngester(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Search Ingester Suite")
}
