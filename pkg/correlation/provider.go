package correlation

import "context"

// Provider is an immutable selection of one history source. A fresh
// Correlator is created for every operation so callers can safely reuse a
// provider across UI refreshes and concurrent robot requests.
type Provider struct {
	mode           HistoryMode
	repositoryRoot string
	issuePath      string
	configPath     string
}

// Mode reports the resolved source kind for diagnostics and capability gates.
func (p *Provider) Mode() string {
	if p == nil {
		return string(HistoryModeOff)
	}
	return string(p.mode)
}

// Enabled reports whether the provider can produce history beyond empty
// off-mode metadata.
func (p *Provider) Enabled() bool {
	return p != nil && p.mode != HistoryModeOff
}

// External reports whether history comes from the Hub ledger and repositories.
func (p *Provider) External() bool {
	return p != nil && p.mode == HistoryModeExternal
}

// NewGitProvider correlates against repositoryRoot, following issuePath when
// it is supplied. issuePath should be the selected JSONL export, not a DB.
func NewGitProvider(repositoryRoot, selectedIssuePath string) *Provider {
	return &Provider{mode: HistoryModeGit, repositoryRoot: repositoryRoot, issuePath: selectedIssuePath}
}

// NewExternalProvider correlates using the authoritative Hub configuration.
func NewExternalProvider(configPath string) *Provider {
	return &Provider{mode: HistoryModeExternal, configPath: configPath}
}

// NewDisabledProvider returns a provider whose report has the same empty
// history metadata as the existing --history-mode=off path.
func NewDisabledProvider() *Provider {
	return &Provider{mode: HistoryModeOff}
}

func (p *Provider) correlator(ctx context.Context) *Correlator {
	if p == nil {
		return NewDisabledProvider().correlator(ctx)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	c := NewCorrelator(p.repositoryRoot, p.issuePath)
	c.historyMode = p.mode
	c.hubConfigPath = p.configPath
	c.ctx = ctx
	c.extractor.ctx = ctx
	c.coCommitter.ctx = ctx
	return c
}

// GenerateReport builds a history report with the operation's context.
func (p *Provider) GenerateReport(ctx context.Context, beads []BeadInfo, opts CorrelatorOptions) (*HistoryReport, error) {
	return p.correlator(ctx).GenerateReport(beads, opts)
}

// GenerateReportCached builds a history report using the existing persistent
// cache behavior for local Git history.
func (p *Provider) GenerateReportCached(ctx context.Context, beads []BeadInfo, opts CorrelatorOptions) (*HistoryReport, error) {
	return p.correlator(ctx).GenerateReportCached(beads, opts)
}
