package main

import (
	cfg "github.com/conductorone/baton-eks/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/config"
)

func main() {
	config.Generate("eks", cfg.Config)
}
