package main

import (
	"fmt"
	"log"

	"github.com/ServersUp/servers-up-backend/internal/common"
)

func main() {
	log.Println("Fargate App starting...")
	// Example of using shared common logic
	user, _ := common.GetData("fargate-user-123")
	fmt.Printf("Fetched shared data for user: %s", user.Email)
	// Application's main loop/server start goes here
	// ...
}
