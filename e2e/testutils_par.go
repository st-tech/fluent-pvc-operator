package e2e

import (
	"fmt"

	ginkgoConfig "github.com/onsi/ginkgo/config"
)

func GinkgoNodeId() string {
	return fmt.Sprintf("%d/%d",
		ginkgoConfig.GinkgoConfig.ParallelNode,
		ginkgoConfig.GinkgoConfig.ParallelTotal,
	)
}

