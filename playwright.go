package main

import (
	"context"

	"github.com/playwright-community/playwright-go"
)

func AddPageLocator(ctx context.Context, locator playwright.Locator, cb func(ctx context.Context, locator playwright.Locator) error) error {
	page, err := locator.Page()
	if err != nil {
		return err
	}
	if err := page.AddLocatorHandler(locator, func(l playwright.Locator) {
		if err := cb(ctx, l); err != nil {
			_ = page.Close(playwright.PageCloseOptions{
				Reason: playwright.String(err.Error()),
			})
		}
	}); err != nil {
		return err
	}
	return nil
}
