package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/itaywol/adeptability/internal/exchange"
	"github.com/itaywol/adeptability/pkg/adept"
)

// newExchangeCmd registers `adept exchange {serve,register,submit,list,show,
// respond,close,reopen,token}` — a team "expertise billboard". One person
// hosts `serve` on a common host; everyone else registers with the bootstrap
// token and posts/answers requests. The server is passive storage + auth; it
// never runs an agent. Agents participate by calling these commands with --json.
func newExchangeCmd(d *Deps) *cobra.Command {
	var server string
	c := &cobra.Command{Use: "exchange", Short: "Team expertise billboard (request/answer board)"}
	// --server is shared by every client subcommand; serve ignores it.
	c.PersistentFlags().StringVar(&server, "server", "", "billboard server URL (default: $ADEPT_EXCHANGE_SERVER or last registered)")
	c.AddCommand(
		newExchangeServeCmd(d),
		newExchangeRegisterCmd(d, &server),
		newExchangeSubmitCmd(d, &server),
		newExchangeListCmd(d, &server),
		newExchangeShowCmd(d, &server),
		newExchangeRespondCmd(d, &server),
		newExchangeStatusCmd(d, &server, "close", adept.ExchangeStatusClosed, "Close a request you authored"),
		newExchangeStatusCmd(d, &server, "reopen", adept.ExchangeStatusAttention, "Reopen a request you authored"),
		newExchangeTokenCmd(d, &server),
		newExchangeSetupStatusCmd(d, &server),
		newExchangeRecommendationCmd(d),
	)
	return c
}

// client builds an authenticated client from the resolved server + stored token.
func (d *Deps) exchangeClient(serverFlag string) (*exchange.Client, string, error) {
	cs, err := d.ExchangeCreds()
	if err != nil {
		return nil, "", err
	}
	server, err := cs.ResolveServer(serverFlag)
	if err != nil {
		return nil, "", err
	}
	token, err := cs.ResolveToken(server)
	if err != nil {
		return nil, "", err
	}
	return exchange.NewClient(server, token, nil), server, nil
}

// ---------- serve ----------

func newExchangeServeCmd(d *Deps) *cobra.Command {
	var addr, db, data string
	var rotateBootstrap bool
	c := &cobra.Command{
		Use:   "serve",
		Short: "Host the billboard server",
		Args:  cobra.NoArgs,
		Long: "Starts the billboard HTTP API. On first run it mints a bootstrap token and prints it once " +
			"(teammates need it to `register`). Serve plain HTTP on a trusted network; terminate TLS at a reverse proxy when exposed.",
	}
	c.Flags().StringVar(&addr, "addr", ":8080", "listen address")
	c.Flags().StringVar(&db, "db", "fs", "storage driver: fs|memory")
	c.Flags().StringVar(&data, "data", "", "data directory for the fs driver (default: <library>/exchange-data)")
	c.Flags().BoolVar(&rotateBootstrap, "rotate-bootstrap", false, "mint a new bootstrap token (invalidates the old one)")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		w := cmd.OutOrStdout()
		if data == "" {
			root, err := d.ResolveLibraryRoot()
			if err != nil {
				return err
			}
			data = filepath.Join(root, "exchange-data")
		}
		store, err := d.ExchangeDrivers.Open(db, data)
		if err != nil {
			return err
		}
		token, err := exchange.EnsureBootstrap(store, rotateBootstrap)
		if err != nil {
			return fmt.Errorf("init bootstrap token: %w", err)
		}
		if token != "" {
			fmt.Fprintf(w, "bootstrap token (shown once — share with teammates to register):\n  %s\n", token)
		} else {
			fmt.Fprintln(w, "reusing existing bootstrap token (pass --rotate-bootstrap to mint a new one)")
		}

		ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return fmt.Errorf("listen %s: %w", addr, err)
		}
		srv := &http.Server{Handler: exchange.NewServer(store), ReadHeaderTimeout: 10 * time.Second}
		errCh := make(chan error, 1)
		go func() { errCh <- srv.Serve(ln) }()
		fmt.Fprintf(w, "billboard serving on %s (db=%s); Ctrl-C to stop\n", ln.Addr().String(), db)

		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("serve: %w", err)
			}
			return nil
		case <-ctx.Done():
			shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			fmt.Fprintln(w, "\nshutting down…")
			return srv.Shutdown(shutCtx)
		}
	}
	return c
}

// ---------- register ----------

func newExchangeRegisterCmd(d *Deps, server *string) *cobra.Command {
	var bootstrap, handle string
	c := &cobra.Command{
		Use:   "register",
		Short: "Register a handle and store a bearer token",
		Args:  cobra.NoArgs,
	}
	c.Flags().StringVar(&bootstrap, "bootstrap", "", "bootstrap token from `serve` (required)")
	c.Flags().StringVar(&handle, "handle", "", "your handle (default: current OS user)")
	_ = c.MarkFlagRequired("bootstrap")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		cs, err := d.ExchangeCreds()
		if err != nil {
			return err
		}
		srv, err := cs.ResolveServer(*server)
		if err != nil {
			return err
		}
		if handle == "" {
			handle = defaultHandle()
		}
		client := exchange.NewClient(srv, "", nil)
		token, err := client.Register(cmd.Context(), bootstrap, handle)
		if err != nil {
			return err
		}
		if err := cs.Save(exchange.Creds{Server: srv, Handle: handle, Token: token}); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "registered as %q on %s\n", handle, srv)
		return nil
	}
	return c
}

// ---------- submit ----------

func newExchangeSubmitCmd(d *Deps, server *string) *cobra.Command {
	var title, body string
	var assignees, tags []string
	c := &cobra.Command{
		Use:   "submit",
		Short: "Submit a request for expertise",
		Args:  cobra.NoArgs,
	}
	c.Flags().StringVar(&title, "title", "", "short request title (required)")
	c.Flags().StringVar(&body, "body", "", "request detail")
	c.Flags().StringArrayVar(&assignees, "assignee", nil, "handle whose expertise you want (repeatable)")
	c.Flags().StringArrayVar(&tags, "tag", nil, "topic tag (repeatable)")
	_ = c.MarkFlagRequired("title")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		client, _, err := d.exchangeClient(*server)
		if err != nil {
			return err
		}
		item, err := client.CreateItem(cmd.Context(), title, body, assignees, tags)
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &exchangeItemRenderable{Item: item})
	}
	return c
}

// ---------- list ----------

func newExchangeListCmd(d *Deps, server *string) *cobra.Command {
	var mine bool
	var status string
	c := &cobra.Command{
		Use:   "list",
		Short: "List billboard requests",
		Args:  cobra.NoArgs,
	}
	c.Flags().BoolVar(&mine, "mine", false, "only requests you authored or are assigned to")
	c.Flags().StringVar(&status, "status", "", "filter by status: attention-required|in-progress|closed")
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		client, _, err := d.exchangeClient(*server)
		if err != nil {
			return err
		}
		items, err := client.ListItems(cmd.Context(), mine, status)
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &exchangeListRenderable{Items: items})
	}
	return c
}

// ---------- show ----------

func newExchangeShowCmd(d *Deps, server *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "show <id>",
		Short: "Show a request and its responses",
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		client, _, err := d.exchangeClient(*server)
		if err != nil {
			return err
		}
		item, err := client.GetItem(cmd.Context(), id)
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &exchangeItemRenderable{Item: item, Full: true})
	}
	return c
}

// ---------- respond ----------

func newExchangeRespondCmd(d *Deps, server *string) *cobra.Command {
	var body string
	c := &cobra.Command{
		Use:   "respond <id>",
		Short: "Post a response on a request",
		Args:  cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&body, "body", "", "your response (required)")
	_ = c.MarkFlagRequired("body")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		client, _, err := d.exchangeClient(*server)
		if err != nil {
			return err
		}
		item, err := client.AddComment(cmd.Context(), id, body)
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &exchangeItemRenderable{Item: item, Full: true})
	}
	return c
}

// ---------- close / reopen ----------

func newExchangeStatusCmd(d *Deps, server *string, use, status, short string) *cobra.Command {
	c := &cobra.Command{
		Use:   use + " <id>",
		Short: short,
		Args:  cobra.ExactArgs(1),
	}
	c.RunE = func(cmd *cobra.Command, args []string) error {
		id, err := strconv.Atoi(args[0])
		if err != nil {
			return fmt.Errorf("invalid id %q", args[0])
		}
		client, _, err := d.exchangeClient(*server)
		if err != nil {
			return err
		}
		item, err := client.SetStatus(cmd.Context(), id, status)
		if err != nil {
			return err
		}
		return d.Print(cmd.OutOrStdout(), &exchangeItemRenderable{Item: item})
	}
	return c
}

// ---------- token ----------

func newExchangeTokenCmd(d *Deps, server *string) *cobra.Command {
	c := &cobra.Command{Use: "token", Short: "Manage your bearer token"}
	rotate := &cobra.Command{
		Use:   "rotate",
		Short: "Rotate your token (the old one stops working immediately)",
		Args:  cobra.NoArgs,
	}
	rotate.RunE = func(cmd *cobra.Command, _ []string) error {
		cs, err := d.ExchangeCreds()
		if err != nil {
			return err
		}
		client, srv, err := d.exchangeClient(*server)
		if err != nil {
			return err
		}
		newToken, err := client.Rotate(cmd.Context())
		if err != nil {
			return err
		}
		cur, _ := cs.Load(srv) // preserve handle; best-effort
		if err := cs.Save(exchange.Creds{Server: srv, Handle: cur.Handle, Token: newToken}); err != nil {
			return err
		}
		fmt.Fprintf(cmd.OutOrStdout(), "token rotated for %s\n", srv)
		return nil
	}
	c.AddCommand(rotate)
	return c
}

// ---------- status / dismiss (used by the expertise-exchange skill) ----------

// newExchangeSetupStatusCmd reports local exchange setup state so the
// expertise-exchange skill can decide whether to prompt the user. It makes no
// network call.
func newExchangeSetupStatusCmd(d *Deps, server *string) *cobra.Command {
	c := &cobra.Command{
		Use:   "status",
		Short: "Report local exchange setup state (configured/registered/dismissed)",
		Args:  cobra.NoArgs,
	}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		cs, err := d.ExchangeCreds()
		if err != nil {
			return err
		}
		srv := *server
		if srv == "" {
			if env := os.Getenv(adept.ExchangeServerEnvVar); env != "" {
				srv = env
			} else {
				srv = cs.DefaultServer()
			}
		}
		return d.Print(cmd.OutOrStdout(), &exchangeStatusRenderable{
			Server:     srv,
			Registered: cs.Registered(srv),
			Dismissed:  cs.RecommendationDismissed(),
		})
	}
	return c
}

// newExchangeRecommendationCmd groups `recommendation {dismiss,undismiss}`,
// the per-user toggle for whether the expertise-exchange skill prompts to set
// up an exchange.
func newExchangeRecommendationCmd(d *Deps) *cobra.Command {
	c := &cobra.Command{Use: "recommendation", Short: "Manage the exchange setup recommendation"}
	c.AddCommand(newExchangeDismissCmd(d, true), newExchangeDismissCmd(d, false))
	return c
}

// newExchangeDismissCmd builds either `dismiss` or `undismiss`.
func newExchangeDismissCmd(d *Deps, dismiss bool) *cobra.Command {
	use, short := "undismiss", "Re-enable the exchange setup recommendation"
	if dismiss {
		use, short = "dismiss", "Stop the exchange setup recommendation (saved per-user)"
	}
	c := &cobra.Command{Use: use, Short: short, Args: cobra.NoArgs}
	c.RunE = func(cmd *cobra.Command, _ []string) error {
		cs, err := d.ExchangeCreds()
		if err != nil {
			return err
		}
		if dismiss {
			if err := cs.DismissRecommendation(); err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), "exchange recommendation dismissed (re-enable with `adept exchange recommendation undismiss`)")
			return nil
		}
		if err := cs.UndismissRecommendation(); err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), "exchange recommendation re-enabled")
		return nil
	}
	return c
}

// defaultHandle derives a fallback handle from the current OS user.
func defaultHandle() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return "anonymous"
}

// ---------- renderables ----------

type exchangeItemRenderable struct {
	Item adept.ExchangeItem
	Full bool
}

func (r *exchangeItemRenderable) JSON() any { return r.Item }
func (r *exchangeItemRenderable) Plain(w io.Writer) error {
	it := r.Item
	fmt.Fprintf(w, "#%d [%s] %s\n", it.ID, it.Status, it.Title)
	fmt.Fprintf(w, "author: %s", it.Author)
	if len(it.Assignees) > 0 {
		fmt.Fprintf(w, "  assignees: %s", strings.Join(it.Assignees, ", "))
	}
	if len(it.Tags) > 0 {
		fmt.Fprintf(w, "  tags: %s", strings.Join(it.Tags, ", "))
	}
	fmt.Fprintln(w)
	if r.Full {
		if it.Body != "" {
			fmt.Fprintf(w, "\n%s\n", it.Body)
		}
		fmt.Fprintf(w, "\nresponses (%d):\n", len(it.Comments))
		for _, c := range it.Comments {
			fmt.Fprintf(w, "  - %s (%s): %s\n", c.Author, c.CreatedAt, c.Body)
		}
	}
	return nil
}

type exchangeStatusRenderable struct {
	Server     string `json:"server"`
	Registered bool   `json:"registered"`
	Dismissed  bool   `json:"dismissed"`
}

func (r *exchangeStatusRenderable) JSON() any { return r }
func (r *exchangeStatusRenderable) Plain(w io.Writer) error {
	server := r.Server
	if server == "" {
		server = "(none configured)"
	}
	fmt.Fprintf(w, "server:     %s\n", server)
	fmt.Fprintf(w, "registered: %t\n", r.Registered)
	fmt.Fprintf(w, "dismissed:  %t\n", r.Dismissed)
	return nil
}

type exchangeListRenderable struct {
	Items []adept.ExchangeItem
}

func (r *exchangeListRenderable) JSON() any { return r.Items }
func (r *exchangeListRenderable) Plain(w io.Writer) error {
	tw := NewTabWriter(w)
	fmt.Fprintln(tw, "ID\tSTATUS\tAUTHOR\tRESPONSES\tTITLE")
	for _, it := range r.Items {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%d\t%s\n", it.ID, it.Status, it.Author, len(it.Comments), truncate(it.Title, 60))
	}
	return tw.Flush()
}
