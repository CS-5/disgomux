package disgomux

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
	"github.com/sahilm/fuzzy"
)

type (
	// Mux is the multiplexer object. Initialized with New().
	Mux struct {
		Prefix         string
		Commands       map[string]Command
		SimpleCommands map[string]SimpleCommand
		Middleware     []Middleware
		options        *Options
		fuzzyMatch     bool
		commandNames   []string
		errorTexts     ErrorTexts
	}

	// Command specifies the functions for a multiplexed command
	Command interface {
		Init(m *Mux)
		Handle(ctx *Context)
		HandleHelp(ctx *Context) bool
		Settings() *CommandSettings
		Permissions() *CommandPermissions
	}

	// CommandPermissions holds permissions for a given command in whitelist
	// format. UserID takes priority over all other permissions. RoleID takes
	// priority over ChanID.
	CommandPermissions struct {
		UserIDs []string
		RoleIDs []string
		ChanIDs []string
	}

	// CommandSettings contain command-specific settings the multiplexer should
	// know.
	CommandSettings struct {
		Command, HelpText string
	}

	// SimpleCommand contains the content and helptext of a logic-less command.
	// Simple commands have no support for permissions.
	SimpleCommand struct {
		Command, Content, HelpText string
	}

	// ErrorTexts holds strings used when an error occurs
	ErrorTexts struct {
		CommandNotFound, NoPermissions string
	}

	// Context is the contexual values supplied to middlewares and handlers
	Context struct {
		Prefix, Command string
		Arguments       []string
		Session         *discordgo.Session
		Message         *discordgo.MessageCreate
	}

	// Middleware specifies a special middleware function that is called anytime
	// handle() is called from DiscordGo
	Middleware func(*Context)

	// Options is a set of config options to use when handling a message. All
	// properties true by default.
	Options struct {
		IgnoreBots       bool
		IgnoreDMs        bool
		IgnoreEmpty      bool
		IgnoreNonDefault bool
	}
)

// New initlaizes a new Mux object
func New(prefix string) (*Mux, error) {
	if len(prefix) > 1 {
		return &Mux{}, fmt.Errorf("Prefix %s greater than 1 character", prefix)
	}

	return &Mux{
		Prefix:         prefix,
		Commands:       make(map[string]Command),
		SimpleCommands: make(map[string]SimpleCommand),
		Middleware:     []Middleware{},
		errorTexts: ErrorTexts{
			CommandNotFound: "Command not found.",
			NoPermissions:   "You do not have permission to use that command.",
		},
		options:    &Options{true, true, true, true},
		fuzzyMatch: false,
	}, nil
}

// Options allows configuration of the multiplexer. Must be called before
// Initialize()
func (m *Mux) Options(opt *Options) {
	m.options = opt
}

// UseMiddleware adds a middleware to the multiplexer. //TODO: Improve this desc
func (m *Mux) UseMiddleware(mw Middleware) {
	m.Middleware = append(m.Middleware, mw)
}

// SetErrors sets the error texts for the multiplexer using the supplied struct
func (m *Mux) SetErrors(errorTexts ErrorTexts) {
	m.errorTexts = errorTexts
}

// Register registers one or more commands to the multiplexer
func (m *Mux) Register(commands ...Command) {
	for _, c := range commands {
		cString := c.Settings().Command
		if len(cString) != 0 {
			m.Commands[cString] = c
		}
	}
}

// RegisterSimple registers one or more simple commands to the multiplexer
func (m *Mux) RegisterSimple(simpleCommands ...SimpleCommand) {
	for _, c := range simpleCommands {
		cString := c.Command
		if len(cString) != 0 {
			m.SimpleCommands[cString] = c
		}
	}
}

// InitializeFuzzy both enables and builds a list of commands to fuzzy match
// against. This _will_ mean taking a performance hit, so use with caution.
func (m *Mux) InitializeFuzzy() {
	m.fuzzyMatch = true

	for k := range m.Commands {
		m.commandNames = append(m.commandNames, k)
	}
}

// Initialize calls the init functions of all registered commands to do any
// preloading or setup before commands are to be handled. Must be called before
// Mux.Handle() and after Mux.Register()
func (m *Mux) Initialize(commands ...Command) {
	/* If no commands are loaded, and none are specified, return */
	if len(commands) == 0 && len(m.Commands) == 0 {
		return
	}

	/* If no commands are specified, init the loaded ones */
	if len(commands) == 0 {
		for _, c := range m.Commands {
			c.Init(m)
		}
		return
	}

	/* Init the specified commands */
	for _, c := range commands {
		c.Init(m)
	}
}

// Handle is passed to DiscordGo to handle actions
func (m *Mux) Handle(
	session *discordgo.Session,
	message *discordgo.MessageCreate,
) {
	/* Ignore if the message being handled originated from the bot */
	if message.Author.ID == session.State.User.ID {
		return
	}

	/* Ignore if the message has no content */
	if m.options.IgnoreEmpty && len(message.Content) == 0 {
		return
	}

	/* Ignore if the message is not default */
	if m.options.IgnoreNonDefault && message.Type != discordgo.MessageTypeDefault {
		return
	}

	/* Ignore if the message originated from a bot */
	if m.options.IgnoreBots && message.Author.Bot {
		return
	}

	/* Ignore if the message is in a DM */
	if m.options.IgnoreDMs && message.GuildID == "" {
		return
	}

	/* Ignore if the message doesn't have the prefix */
	if !strings.HasPrefix(message.Content, m.Prefix) {
		return
	}

	/* Split the message on the space */
	args := strings.Split(message.Content, " ")
	command := strings.ToLower(args[0][1:])

	simple, ok := m.SimpleCommands[command]
	if ok {
		session.ChannelMessageSend(message.ChannelID, simple.Content)
		return
	}

	handler, ok := m.Commands[command]
	if !ok {
		if m.fuzzyMatch {
			var sb strings.Builder

			for _, fzy := range fuzzy.Find(command, m.commandNames) {
				sb.WriteString("- `!" + fzy.Str + "`\n")
			}

			if sb.Len() != 0 {
				session.ChannelMessageSend(
					message.ChannelID,
					fmt.Sprintf(
						"Command not found. Did you mean: \n%s", sb.String(),
					),
				)
				return
			}

		}

		session.ChannelMessageSend(
			message.ChannelID,
			m.errorTexts.CommandNotFound,
		)

		return
	}

	ctx := &Context{
		Prefix:    m.Prefix,
		Command:   command,
		Arguments: args[1:],
		Session:   session,
		Message:   message,
	}

	/* Call middlewares */
	if len(m.Middleware) > 0 {
		for _, mw := range m.Middleware {
			mw(ctx)
		}
	}

	p := handler.Permissions()
	if len(p.RoleIDs) != 0 {
		member, err := session.GuildMember(message.GuildID, message.Author.ID)
		if err != nil {
			session.ChannelMessageSend(
				message.ChannelID,
				"There was a weird issue. Maybe report it on Github?",
			)
			return
		}

		/* Check if user explicitly has permission */
		if arrayContains(p.UserIDs, member.User.ID) {
			go handler.Handle(ctx)
			return
		}

		/* Check if one of the user's roles has permission */
		for _, r := range member.Roles {
			if arrayContains(p.RoleIDs, r) {
				go handler.Handle(ctx)
				return
			}
		}

		/* Check if the channel has permission */
		if arrayContains(p.ChanIDs, message.ChannelID) {
			go handler.Handle(ctx)
			return
		}

		/* Clearly the user doesn't have the correct permissions */
		session.ChannelMessageSend(
			message.ChannelID, m.errorTexts.NoPermissions,
		)
		return
	}
	go handler.Handle(ctx)
}

// ChannelSend is a helper function for easily sending a message to the current
// channel.
func (ctx *Context) ChannelSend(message string) (*discordgo.Message, error) {
	return ctx.Session.ChannelMessageSend(ctx.Message.ChannelID, message)
}

// ChannelSendf is a helper function like ChannelSend for sending a formatted
// message to the current channel.
func (ctx *Context) ChannelSendf(
	format string,
	a ...interface{},
) (*discordgo.Message, error) {
	return ctx.Session.ChannelMessageSend(
		ctx.Message.ChannelID, fmt.Sprintf(format, a...),
	)
}

func arrayContains(array []string, value string) bool {
	for _, e := range array {
		if e == value {
			return true
		}
	}
	return false
}
