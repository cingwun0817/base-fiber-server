package main

import (
	"base-fiber-server/internal/common"
	"base-fiber-server/routes"
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"base-fiber-server/internal/logger"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/swagger"
	"github.com/spf13/viper"

	_ "base-fiber-server/docs"
)

// @title           adGeek DMP Collect
// @version         1.0
// @description     This is collect server.
// @host            localhost:3000
// @BasePath        /
func main() {
	common.LoadConfig()
	logger.InitLoggers()

	// init
	app := fiber.New(fiber.Config{
		Prefork: true,
		ErrorHandler: func(ctx *fiber.Ctx, err error) error {
			code := fiber.StatusInternalServerError
			if e, ok := err.(*fiber.Error); ok {
				code = e.Code
			}

			msg := common.RequestErrorMessage{
				Time:    time.Now().Format("2006-01-02 15:04:05"),
				Request: fmt.Sprintf("%s %s", ctx.Method(), ctx.OriginalURL()),
				Code:    code,
				Error:   err.Error(),
			}

			logger.AsyncError(msg)

			return nil
		},
	})

	// swagger (https://github.com/gofiber/swagger)
	app.Get("/swagger/*", swagger.HandlerDefault)

	// cors (https://docs.gofiber.io/api/middleware/cors)
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
	}))

	// error (https://docs.gofiber.io/guide/error-handling)
	app.Use(recover.New())

	app.Use(cors.New())

	// route
	routes.New(app)

	// start the server on port
	go gracefulShutdown(app)

	go func() {
		if err := app.Listen(":" + viper.GetString("app.port")); err != nil {
			logger.AsyncError("server error: " + err.Error())
			os.Exit(1)
		}
	}()

	if os.Getenv("FIBER_CHILD") != "1" {
		select {}
	}
}

func gracefulShutdown(app *fiber.App) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
	fmt.Printf("[PID %d] Shutting down server...\n", os.Getpid())

	if os.Getenv("FIBER_CHILD") == "1" {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := app.ShutdownWithContext(shutdownCtx); err != nil {
			logger.AsyncError("shutdown failed: " + err.Error())
			os.Exit(1)
		}
	}

	logger.Close()
	fmt.Printf("[PID %d] Log files closed, Bye!\n", os.Getpid())
	os.Exit(0)
}
