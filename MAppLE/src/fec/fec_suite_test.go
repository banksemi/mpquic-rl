package fec_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestFec(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fec Suite")
}
