package routes

import (
	"github.com/gofiber/fiber/v2"
)

func New(app *fiber.App) {
	app.Get("/favicon.ico", func(c *fiber.Ctx) error {
		return c.SendStatus(fiber.StatusNoContent)
	})
}
