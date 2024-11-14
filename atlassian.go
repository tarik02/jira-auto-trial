package main

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
	"golang.org/x/sync/errgroup"
)

type AtlassianLoginHandler struct {
	UsernameResolver func(ctx context.Context) (string, error)
	PasswordResolver func(ctx context.Context) (string, error)
	OTPCodeResolver  func(ctx context.Context) (string, error)
}

func (s *AtlassianLoginHandler) Run(ctx context.Context, page playwright.Page) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return RunPageLocator(ctx, page.Locator(`//form[@data-testid="form-login"]//input[@data-testid="username"]`), func(ctx context.Context, locator playwright.Locator) error {
			username, err := s.UsernameResolver(ctx)
			if err != nil {
				return err
			}

			if err := page.Locator(`//form[@data-testid="form-login"]//input[@data-testid="username"]`).Fill(username); err != nil {
				return err
			}

			return page.Locator(`//form[@data-testid="form-login"]//*[@type="submit"]`).Click()
		})
	})

	g.Go(func() error {
		return RunPageLocator(ctx, page.Locator(`//form[@data-testid="form-login"]//input[@data-testid="password"]`), func(ctx context.Context, locator playwright.Locator) error {
			password, err := s.PasswordResolver(ctx)
			if err != nil {
				return err
			}

			if err := page.Locator(`//form[@data-testid="form-login"]//input[@data-testid="password"]`).Fill(password); err != nil {
				return err
			}

			return page.Locator(`//form[@data-testid="form-login"]//*[@type="submit"]`).Click()
		})
	})

	g.Go(func() error {
		return RunPageLocator(ctx, page.Locator(`//form//input[@id="two-step-verification-otp-code-input" and not(@disabled)]`), func(ctx context.Context, locator playwright.Locator) error {
			otpCode, err := s.OTPCodeResolver(ctx)
			if err != nil {
				return err
			}

			return page.Locator(`//form//input[@id="two-step-verification-otp-code-input" and not(@disabled)]`).Fill(otpCode)
		})
	})

	g.Go(func() error {
		return RunPageLocator(ctx, page.Locator(`//*[text()="Continue without two-step verification"]`), func(ctx context.Context, locator playwright.Locator) error {
			return page.Locator(`//*[text()="Continue without two-step verification"]`).Click()
		})
	})

	return g.Wait()
}

type GetLicenseKeyParams struct {
	ServerID string
}

func GetLicenseKey(ctx context.Context, page playwright.Page, params GetLicenseKeyParams) (string, error) {
	if _, err := page.Goto("https://my.atlassian.com/license/evaluation"); err != nil {
		return "", fmt.Errorf("could not navigate: %w", err)
	}

	if err := page.Locator(`//select[@id="product-select"]`).Click(); err != nil {
		return "", fmt.Errorf("could not select product: %w", err)
	}

	if _, err := page.Locator(`//select[@id="product-select"]`).SelectOption(playwright.SelectOptionValues{
		Values: &[]string{"Jira"},
	}, playwright.LocatorSelectOptionOptions{Force: playwright.Bool(true)}); err != nil {
		return "", fmt.Errorf("could not select product: %w", err)
	}

	time.Sleep(1 * time.Second)

	if err := page.Locator(`//*[@data="jira-software.data-center"]//*[text()="Select"]`).Click(); err != nil {
		return "", fmt.Errorf("could not select DC: %w", err)
	}

	time.Sleep(1 * time.Second)

	if err := page.Locator(`//*[@data="jira-software.data-center"]//*[contains(concat(" ", text(), " "), " aui-button-primary ")]`).Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(2),
	}); err != nil && !errors.Is(err, playwright.ErrTimeout) {
		return "", fmt.Errorf("could not select DC: %w", err)
	}

	time.Sleep(1 * time.Second)

	if err := page.Locator(`//*[@data="jira-software.data-center"]//*[contains(concat(" ", text(), " "), " aui-button-primary ")]`).Click(playwright.LocatorClickOptions{
		Timeout: playwright.Float(2),
	}); err != nil && !errors.Is(err, playwright.ErrTimeout) {
		return "", fmt.Errorf("could not select DC: %w", err)
	}

	if err := page.Locator(`//input[@name="sid"]`).Fill(params.ServerID); err != nil {
		return "", fmt.Errorf("could not type in server id: %w", err)
	}

	if err := page.Locator(`//input[@name="_action_evaluation"]`).Click(); err != nil {
		return "", fmt.Errorf("could generate license: %w", err)
	}

	if err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
		State: playwright.LoadStateLoad,
	}); err != nil {
		return "", fmt.Errorf("could not wait for load state: %w", err)
	}

	url, err := url.Parse(page.URL())
	if err != nil {
		return "", fmt.Errorf("could not parse page url: %w", err)
	}

	licenseKey, err := page.Locator(fmt.Sprintf(`//tr[@id="%s"]/following::tr[@class="evaluation"][1]//textarea`, url.Fragment)).InputValue()
	if err != nil {
		return "", fmt.Errorf("could not find license key: %w", err)
	}

	licenseKey = strings.ReplaceAll(licenseKey, "\n", "")

	return licenseKey, nil
}
