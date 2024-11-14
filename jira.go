package main

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"
	"golang.org/x/sync/errgroup"
)

type JiraLoginHandler struct {
	CredentialsResolver func(ctx context.Context) (string, string, error)
	RememberMe          bool
}

func TimeParseAny(formats []string, value string) (time.Time, error) {
	errs := make([]error, 0)
	for _, format := range formats {
		if date, err := time.Parse(format, value); err != nil {
			errs = append(errs, err)
		} else {
			return date, nil
		}
	}

	return time.Time{}, errors.Join(errs...)
}

func RunPageLocator(ctx context.Context, locator playwright.Locator, cb func(ctx context.Context, locator playwright.Locator) error, options ...playwright.PageAddLocatorHandlerOptions) error {
	page, err := locator.Page()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancelCause(ctx)
	if err := page.AddLocatorHandler(locator, func(l playwright.Locator) {
		if err := cb(ctx, l); err != nil {
			cancel(err)
		}
	}, options...); err != nil {
		return err
	}

	<-ctx.Done()

	_ = page.RemoveLocatorHandler(locator)
	return ctx.Err()
}

func (s *JiraLoginHandler) Run(ctx context.Context, page playwright.Page) error {
	g, ctx := errgroup.WithContext(ctx)

	g.Go(func() error {
		return RunPageLocator(ctx, page.Locator(`//form[contains(@action, "/login.jsp")]`), func(ctx context.Context, locator playwright.Locator) error {
			username, password, err := s.CredentialsResolver(ctx)
			if err != nil {
				return err
			}
			if err := locator.Locator(`[name="os_password"]`).Fill(password); err != nil {
				return err
			}
			if err := locator.Locator(`[name="os_username"]`).First().Fill(username); err != nil {
				return err
			}
			if s.RememberMe {
				if err := locator.Locator(`[for="login-form-remember-me"]`).Check(playwright.LocatorCheckOptions{
					Force: playwright.Bool(true),
				}); err != nil {
					return err
				}
			}
			if err := locator.Locator(`[name="login"]`).Click(); err != nil {
				return err
			}

			if err := locator.WaitFor(playwright.LocatorWaitForOptions{
				State: playwright.WaitForSelectorStateHidden,
			}); err != nil {
				return err
			}

			loginErr, err := page.Locator(`//form[@id="login-form"]//div[contains(concat(' ', @class, ' '), ' aui-message-error ')]`).InnerText(playwright.LocatorInnerTextOptions{
				Timeout: playwright.Float(1000),
			})
			if err != nil {
				if errors.Is(err, playwright.ErrTimeout) {
					return nil
				}
				return err
			}

			return fmt.Errorf("login error: %s", loginErr)
		})
	})

	return g.Wait()
}

type JiraSudoHandler struct {
	PasswordResolver func(ctx context.Context) (string, error)
}

func (s *JiraSudoHandler) Run(ctx context.Context, page playwright.Page) error {
	return RunPageLocator(ctx, page.Locator(`//form[contains(@action, "/WebSudoAuthenticate.jspa")]`), func(ctx context.Context, locator playwright.Locator) error {
		password, err := s.PasswordResolver(ctx)
		if err != nil {
			return err
		}
		if err := locator.Locator(`[name="webSudoPassword"]`).Fill(password); err != nil {
			return err
		}
		if err := locator.Locator(`[type="submit"]`).Click(); err != nil {
			return err
		}

		return nil
	})
}

type ResolveServerIDParams struct {
	BaseURL string
}

func ResolveServerID(ctx context.Context, page playwright.Page, params ResolveServerIDParams) (string, error) {
	if _, err := page.Goto(fmt.Sprintf("%s/secure/admin/ViewSystemInfo.jspa", params.BaseURL)); err != nil {
		return "", fmt.Errorf("could not navigate to system info: %w", err)
	}

	cellLocator := page.Locator(`//tr[td[@class='cell-type-key']/strong[text()='Server ID']]/td[@class='cell-type-value']`)
	if err := cellLocator.Click(); err != nil {
		return "", err
	}

	res, err := cellLocator.TextContent()
	if err != nil {
		return "", fmt.Errorf("error extracting server id from page: %w", err)
	}

	return res, nil
}

type ResolveLicenseDetailsParams struct {
	BaseURL        string
	ApplicationKey string
}

type ResolveLicenseDetailsResult struct {
	TrialExpiresAt   *time.Time
	SEN              string
	LicenseType      string
	OrganisationName string
	LicenseKey       string
}

func ResolveLicenseDetails(ctx context.Context, page playwright.Page, params ResolveLicenseDetailsParams) (*ResolveLicenseDetailsResult, error) {
	applicationKey := params.ApplicationKey
	if applicationKey == "" {
		applicationKey = "jira-software"
	}

	if _, err := page.Goto(fmt.Sprintf("%s/plugins/servlet/applications/versions-licenses", params.BaseURL)); err != nil {
		return nil, fmt.Errorf("could not navigate to licenses: %w", err)
	}

	appLocator := page.Locator(fmt.Sprintf(`//div[@data-application-key="%s"]`, applicationKey))
	if err := appLocator.Click(); err != nil {
		return nil, err
	}

	detailFields, err := appLocator.Locator(`.license-detail-field`).All()
	if err != nil {
		return nil, err
	}

	var result ResolveLicenseDetailsResult

	for _, item := range detailFields {
		name, err := item.Locator("dt").InnerText()
		if err != nil {
			return nil, err
		}

		value, err := item.Locator(`.license-string-raw, dd`).First().TextContent()
		if err != nil {
			return nil, err
		}

		switch name {
		case "Trial expires":
			if date, err := TimeParseAny([]string{"02/Jan/06", "2 Jan 2006"}, value); err != nil {
				return nil, err
			} else {
				result.TrialExpiresAt = &date
			}

		case "Support entitlement number (SEN)":
			result.SEN = value

		case "License type":
			result.LicenseType = value

		case "Organisation name":
			result.OrganisationName = value

		case "License key":
			result.LicenseKey = value
		}
	}

	return &result, nil
}

type UpdateJiraLicenseKeyParams struct {
	BaseURL        string
	ApplicationKey string
	LicenseKey     string
}

func UpdateJiraLicenseKey(ctx context.Context, page playwright.Page, params UpdateJiraLicenseKeyParams) error {
	applicationKey := params.ApplicationKey
	if applicationKey == "" {
		applicationKey = "jira-software"
	}

	if _, err := page.Goto(fmt.Sprintf("%s/plugins/servlet/applications/versions-licenses", params.BaseURL)); err != nil {
		return fmt.Errorf("could not navigate to licenses: %w", err)
	}

	appLocator := page.Locator(fmt.Sprintf(`//div[@data-application-key="%s"]`, applicationKey))

	if err := appLocator.Locator(`//*[@class="update-license-key"]`).Click(); err != nil {
		return err
	}

	if err := appLocator.Locator(`textarea.license-update-textarea`).Fill(params.LicenseKey); err != nil {
		return err
	}

	if err := appLocator.Locator(`.license-update-submit`).Click(); err != nil {
		return err
	}

	if err := page.Locator(`//*[@id="multiple-license-dialog"]//button[text()="Finish" and not(contains(concat(" ", @class, " "), " hidden "))]`).Click(); err != nil && !errors.Is(err, playwright.ErrTimeout) {
		return err
	}

	if err := appLocator.Locator(`textarea.license-update-textarea`).WaitFor(playwright.LocatorWaitForOptions{
		State: playwright.WaitForSelectorStateHidden,
	}); err != nil {
		return err
	}

	// TODO: wait for updated?

	return nil
}
