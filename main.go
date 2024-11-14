package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/playwright-community/playwright-go"
	"github.com/tarik02/jira-auto-trial/config"
	"github.com/tarik02/jira-auto-trial/credentials"
	prettyconsole "github.com/thessem/zap-prettyconsole"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
	"gopkg.in/yaml.v3"
)

func main() {
	logger := prettyconsole.NewLogger(zap.DebugLevel)
	defer logger.Sync()

	ctx := context.Background()
	if err := run(ctx, logger); err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal("error", zap.Error(err))
	}
}

func processInstance(
	ctx context.Context,
	log *zap.Logger,
	jiraPage playwright.Page,
	instance config.JiraInstance,
	getLicenseKey func(context context.Context, serverId string) (string, error),
) error {
	g, ctx := errgroup.WithContext(ctx)

	_ = g.TryGo(func() error {
		return (&JiraLoginHandler{
			CredentialsResolver: func(ctx context.Context) (string, string, error) {
				creds, err := credentials.ResolveCredentials(ctx, instance.Account)
				if err != nil {
					return "", "", err
				}
				return creds.Username, creds.Password, nil
			},
			RememberMe: true,
		}).Run(ctx, jiraPage)
	})

	_ = g.TryGo(func() error {
		return (&JiraSudoHandler{
			PasswordResolver: func(ctx context.Context) (string, error) {
				creds, err := credentials.ResolveCredentials(ctx, instance.Account)
				if err != nil {
					return "", err
				}
				return creds.Password, nil
			},
		}).Run(ctx, jiraPage)
	})

	log.Info("processing instance")

	log.Info("resolving license details")

	licenseDetails, err := ResolveLicenseDetails(ctx, jiraPage, ResolveLicenseDetailsParams{
		BaseURL: instance.BaseURL,
	})
	if err != nil {
		return fmt.Errorf("resolving license details: %w", err)
	}

	trialExpiresAtStr := "-"
	if licenseDetails.TrialExpiresAt != nil {
		trialExpiresAtStr = licenseDetails.TrialExpiresAt.Format(time.DateTime)
	}
	log.Info(
		"license details",
		zap.String("trial expires at", trialExpiresAtStr),
		zap.String("sen", licenseDetails.SEN),
		zap.String("license type", licenseDetails.LicenseType),
		zap.String("organisation name", licenseDetails.OrganisationName),
		zap.String("license key", licenseDetails.LicenseKey),
	)

	if licenseDetails.TrialExpiresAt != nil && !licenseDetails.TrialExpiresAt.Before(time.Now().AddDate(0, 0, 7)) {
		log.Warn("skipping: more than 7 days of trial left")
		return nil
	}

	log.Info("resolving server id")

	serverID, err := ResolveServerID(ctx, jiraPage, ResolveServerIDParams{
		BaseURL: instance.BaseURL,
	})
	if err != nil {
		return fmt.Errorf("resolving server id: %w", err)
	}

	log.Info("server id", zap.String("server id", serverID))

	log.Info("resolving license key")

	licenseKey, err := getLicenseKey(ctx, serverID)
	if err != nil {
		return fmt.Errorf("resolving license key: %w", err)
	}

	log.Info("license key", zap.String("license key", licenseKey))

	if err := UpdateJiraLicenseKey(ctx, jiraPage, UpdateJiraLicenseKeyParams{
		BaseURL:    instance.BaseURL,
		LicenseKey: licenseKey,
	}); err != nil {
		return err
	}

	log.Info("license key updated")

	return nil
}

func run(ctx context.Context, log *zap.Logger) error {
	var cfg config.Config

	if file, err := os.Open("./config.yml"); err != nil {
		return fmt.Errorf("error reading config: %w", err)
	} else {
		defer file.Close()

		if err := yaml.NewDecoder(file).Decode(&cfg); err != nil {
			return fmt.Errorf("error decoding config: %w", err)
		}
	}

	if err := os.MkdirAll("./data", 0700); err != nil {
		return fmt.Errorf("error creating data directory: %w", err)
	}

	runOptions := &playwright.RunOptions{
		DriverDirectory: "./data/playwright",
		Browsers:        []string{"chromium"},
	}

	if err := playwright.Install(runOptions); err != nil {
		return err
	}

	pw, err := playwright.Run(runOptions)
	if err != nil {
		return fmt.Errorf("could not run playwright: %w", err)
	}
	defer pw.Stop()

	var browserContext playwright.BrowserContext

	if ep := cfg.Playwright.Endpoint; ep != "" {
		browser, err := pw.Chromium.ConnectOverCDP(cfg.Playwright.Endpoint)
		if err != nil {
			return fmt.Errorf("could not connect to browser: %w", err)
		}
		defer browser.Close()

		browserContext, err = browser.NewContext()
		if err != nil {
			return fmt.Errorf("error creating browser context: %w", err)
		}
		defer browserContext.Close()
	} else {
		browserContext, err = pw.Chromium.LaunchPersistentContext("./data/browser", playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless: playwright.Bool(!cfg.Playwright.Headful),
		})
		if err != nil {
			return fmt.Errorf("could not launch browser: %w", err)
		}
		defer browserContext.Close()
	}

	jiraPage, err := browserContext.NewPage()
	if err != nil {
		return fmt.Errorf("could not create page: %w", err)
	}
	defer jiraPage.Close()

	ctx, cancel := context.WithCancelCause(ctx)

	rootGroup, ctx := errgroup.WithContext(ctx)

	resolveAtlassianPage := sync.OnceValues(func() (playwright.Page, error) {
		atlassianPage, err := browserContext.NewPage()
		if err != nil {
			return nil, fmt.Errorf("could not create page: %w", err)
		}

		rootGroup.Go(func() error {
			defer atlassianPage.Close()
			<-ctx.Done()
			return nil
		})

		_ = rootGroup.TryGo(func() error {
			return (&AtlassianLoginHandler{
				UsernameResolver: func(ctx context.Context) (string, error) {
					creds, err := credentials.ResolveCredentials(ctx, cfg.Atlassian.Account)
					if err != nil {
						return "", err
					}
					return creds.Username, nil
				},
				PasswordResolver: func(ctx context.Context) (string, error) {
					creds, err := credentials.ResolveCredentials(ctx, cfg.Atlassian.Account)
					if err != nil {
						return "", err
					}
					return creds.Password, nil
				},
				OTPCodeResolver: func(ctx context.Context) (string, error) {
					os.Stdout.WriteString("OTP Code: ")
					reader := bufio.NewReader(os.Stdin)
					text, _ := reader.ReadString('\n')
					text = strings.Replace(text, "\n", "", -1)
					return text, nil
				},
			}).Run(ctx, atlassianPage)
		})

		return atlassianPage, nil
	})

	for _, instance := range cfg.Instances {
		instanceLog := log.With(zap.String("instance", instance.BaseURL))

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		instanceCtx, cancelInstance := context.WithCancel(ctx)
		if err := processInstance(instanceCtx, instanceLog, jiraPage, instance, func(ctx context.Context, serverId string) (string, error) {
			page, err := resolveAtlassianPage()
			if err != nil {
				cancel(err)
				return "", context.Canceled
			}
			return GetLicenseKey(ctx, page, GetLicenseKeyParams{
				ServerID: serverId,
			})
		}); err != nil {
			instanceLog.Error("processing failed", zap.Error(err))
			cancelInstance()
			continue
		}

		cancelInstance()
		instanceLog.Info("processing done")
	}

	cancel(context.Canceled)

	return rootGroup.Wait()
}
