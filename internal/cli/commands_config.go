package cli

import (
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/llm"
	"github.com/itaywol/adeptability/pkg/adept"
)

// newConfigCmd registers `adept config {list,get,set,unset} [...]`
// plus the `adept config llm {set,unset,test}` subgroup.
//
// Scalar keys are routed through a small typed dispatcher so `config
// set scan.blockSeverity garbage` fails at parse time with a clear
// allowed-values list, instead of corrupting the JSON config.
//
// API keys live in env vars, not in the config — `config llm set` only
// records provider+model+endpoint.
func newConfigCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "config", Short: "Read or write project configuration"}
	c.AddCommand(
		newConfigListCmd(d),
		newConfigGetCmd(d),
		newConfigSetCmd(d),
		newConfigUnsetCmd(d),
		newConfigLLMCmd(d),
	)
	return c
}

// ---------- scalar keys ----------
//
// configKey is one typed scalar exposed via `config {get,set,unset}`.
// Each key knows how to read its current value out of an adept.Config,
// validate a user-supplied string, write it back, and clear it.
type configKey struct {
	name    string
	help    string
	allowed []string
	get     func(*adept.Config) string
	set     func(*adept.Config, string) error
	unset   func(*adept.Config)
}

func scalarKeys() []configKey {
	return []configKey{
		{
			name:    "mode",
			help:    "harness materialization mode",
			allowed: []string{"symlink", "copy"},
			get: func(c *adept.Config) string {
				if c.Mode == "" {
					return string(adept.ModeSymlink)
				}
				return string(c.Mode)
			},
			set: func(c *adept.Config, v string) error {
				m := adept.HarnessMode(v)
				if m != adept.ModeSymlink && m != adept.ModeCopy {
					return fmt.Errorf("mode: want symlink|copy, got %q", v)
				}
				c.Mode = m
				return nil
			},
			unset: func(c *adept.Config) { c.Mode = "" },
		},
		{
			name:    "scan.onInstall",
			help:    "run the safety scan + LLM intent pass before `skill install`",
			allowed: []string{"true", "false"},
			get: func(c *adept.Config) string {
				if c.Scan == nil || c.Scan.OnInstall == nil {
					return "(unset, default: on when llm configured)"
				}
				return strconv.FormatBool(*c.Scan.OnInstall)
			},
			set: func(c *adept.Config, v string) error {
				b, err := strconv.ParseBool(v)
				if err != nil {
					return fmt.Errorf("scan.onInstall: want true|false, got %q", v)
				}
				if c.Scan == nil {
					c.Scan = &adept.ScanConfig{}
				}
				c.Scan.OnInstall = &b
				return nil
			},
			unset: func(c *adept.Config) {
				if c.Scan != nil {
					c.Scan.OnInstall = nil
					if c.Scan.BlockSeverity == "" {
						c.Scan = nil
					}
				}
			},
		},
		{
			name:    "scan.blockSeverity",
			help:    "lowest scan severity that aborts an install",
			allowed: []string{"critical", "high", "medium"},
			get: func(c *adept.Config) string {
				if c.Scan == nil || c.Scan.BlockSeverity == "" {
					return "(unset, default: critical)"
				}
				return c.Scan.BlockSeverity
			},
			set: func(c *adept.Config, v string) error {
				v = strings.ToLower(v)
				if v != "critical" && v != "high" && v != "medium" {
					return fmt.Errorf("scan.blockSeverity: want critical|high|medium, got %q", v)
				}
				if c.Scan == nil {
					c.Scan = &adept.ScanConfig{}
				}
				c.Scan.BlockSeverity = v
				return nil
			},
			unset: func(c *adept.Config) {
				if c.Scan != nil {
					c.Scan.BlockSeverity = ""
					if c.Scan.OnInstall == nil {
						c.Scan = nil
					}
				}
			},
		},
	}
}

func findKey(name string) (configKey, bool) {
	for _, k := range scalarKeys() {
		if k.name == name {
			return k, true
		}
	}
	return configKey{}, false
}

func keyNames() []string {
	ks := scalarKeys()
	names := make([]string, 0, len(ks))
	for _, k := range ks {
		names = append(names, k.name)
	}
	sort.Strings(names)
	return names
}

// ---------- config list ----------

func newConfigListCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "list", Short: "List configurable keys and current values", Args: cobra.NoArgs}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		rows := []configRow{}
		for _, k := range scalarKeys() {
			rows = append(rows, configRow{Key: k.name, Value: k.get(cfg), Allowed: k.allowed, Help: k.help})
		}
		// llm is grouped — surface its three sub-fields so `config list`
		// is a complete one-page view.
		if cfg.LLM != nil {
			rows = append(rows,
				configRow{Key: "llm.provider", Value: cfg.LLM.Provider, Help: "current llm provider name"},
				configRow{Key: "llm.model", Value: orDash(cfg.LLM.Model), Help: "model override (provider default when unset)"},
				configRow{Key: "llm.endpoint", Value: orDash(cfg.LLM.Endpoint), Help: "endpoint override (e.g. self-hosted ollama)"},
			)
		} else {
			rows = append(rows, configRow{Key: "llm", Value: "(unset)", Help: "run `adept config llm set <provider>` to enable"})
		}
		return d.Print(cmd.OutOrStdout(), &configListRenderable{Rows: rows})
	}
	return c
}

type configRow struct {
	Key     string   `json:"key"`
	Value   string   `json:"value"`
	Allowed []string `json:"allowed,omitempty"`
	Help    string   `json:"help,omitempty"`
}

type configListRenderable struct{ Rows []configRow }

func (r *configListRenderable) JSON() any { return r.Rows }
func (r *configListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "KEY\tVALUE\tALLOWED\tHELP")
	for _, row := range r.Rows {
		allowed := "-"
		if len(row.Allowed) > 0 {
			allowed = strings.Join(row.Allowed, "|")
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Key, row.Value, allowed, row.Help)
	}
	return tw.Flush()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// ---------- config get ----------

func newConfigGetCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "get <key>",
		Short:             "Print one config value",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: configKeyCompletion(),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		k, ok := findKey(args[0])
		if !ok {
			return fmt.Errorf("unknown key %q (known: %v)", args[0], keyNames())
		}
		fmt.Fprintln(cmd.OutOrStdout(), k.get(cfg))
		return nil
	}
	return c
}

// ---------- config set ----------

func newConfigSetCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "set <key> <value>",
		Short:             "Set one config value (strict-typed)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: configKeyCompletion(),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		k, ok := findKey(args[0])
		if !ok {
			return fmt.Errorf("unknown key %q (known: %v)", args[0], keyNames())
		}
		if err := k.set(cfg, args[1]); err != nil {
			return err
		}
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "set %s=%s\n", args[0], args[1])
		return nil
	}
	return c
}

// ---------- config unset ----------

func newConfigUnsetCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{
		Use:               "unset <key>",
		Short:             "Clear one config value back to default",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: configKeyCompletion(),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		k, ok := findKey(args[0])
		if !ok {
			return fmt.Errorf("unknown key %q (known: %v)", args[0], keyNames())
		}
		k.unset(cfg)
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "unset %s\n", args[0])
		return nil
	}
	return c
}

// configKeyCompletion completes against the scalar key list. The llm
// subgroup has its own dedicated subcommands so it does not surface
// here.
func configKeyCompletion() cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		ks := keyNames()
		out := make([]cobra.Completion, 0, len(ks))
		for _, k := range ks {
			out = append(out, cobra.Completion(k))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// ---------- config llm ----------

func newConfigLLMCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "llm", Short: "Configure the LLM provider used by safety scans"}
	c.AddCommand(newConfigLLMSetCmd(d), newConfigLLMUnsetCmd(d), newConfigLLMTestCmd(d))
	return c
}

func newConfigLLMSetCmd(d *Deps) *cobra.Command {
	var model, endpoint string
	c := &cobra.Command{
		Use:               "set <provider>",
		Short:             "Pick the LLM provider (api key is read from env at call time)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: llmProviderCompletion(d),
	}
	c.Flags().StringVar(&model, "model", "", "override the provider's default model")
	c.Flags().StringVar(&endpoint, "endpoint", "", "override the provider's default endpoint (e.g. self-hosted ollama)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		provider := strings.ToLower(args[0])
		if _, err := d.LLMRegistry.Get(provider); err != nil {
			return err
		}
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		cfg.LLM = &adept.LLMConfig{Provider: provider, Model: model, Endpoint: endpoint}
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "llm provider set to %s (model=%s endpoint=%s)\n",
			provider, orDash(model), orDash(endpoint))
		return nil
	}
	return c
}

func newConfigLLMUnsetCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "unset", Short: "Forget the LLM provider configuration", Args: cobra.NoArgs}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		p, err := d.Project()
		if err != nil {
			return err
		}
		cfg, err := p.Config()
		if err != nil {
			return err
		}
		cfg.LLM = nil
		if err := p.SaveConfig(cfg); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "llm provider cleared")
		return nil
	}
	return c
}

func newConfigLLMTestCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "test", Short: "Health-ping the configured LLM provider", Args: cobra.NoArgs}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		prov := d.LLMProvider()
		if prov == nil {
			return errors.New("no llm provider configured — run `adept config llm set <provider>`")
		}
		if err := prov.Available(cmd.Context()); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "llm provider %q reachable (default model: %s)\n", prov.Name(), prov.DefaultModel())
		return nil
	}
	return c
}

func llmProviderCompletion(d *Deps) cobra.CompletionFunc {
	return func(cmd *cobra.Command, args []string, toComplete string) ([]cobra.Completion, cobra.ShellCompDirective) {
		if len(args) > 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		var names []string
		if d.LLMRegistry != nil {
			names = d.LLMRegistry.List()
		}
		out := make([]cobra.Completion, 0, len(names))
		for _, n := range names {
			out = append(out, cobra.Completion(n))
		}
		return out, cobra.ShellCompDirectiveNoFileComp
	}
}

// Sanity reference so the llm import is used even before the llm gate
// integration lands in commands_skill_external.go.
var _ = llm.NewRegistry
