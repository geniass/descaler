package main

import (
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"

	"k8s.isazi.ai/descaler/controller"
	"k8s.isazi.ai/descaler/handlers"
)

func main() {
	controller.Start(&handlers.Default{})
}
