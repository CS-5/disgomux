package disgomux

import (
	"fmt"
	"strings"

	"github.com/bwmarrin/discordgo"
)

type (
	// Mux is the multiplexer object. Initialized with New().
	Mux struct {
		Prefix     string
		Commands   map[string]Command
		errorTexts ErrorTexts
		logger     Logger
	}

	// HandlerFunc is a placeholder type for defining middlewares or handlers
	HandlerFunc func(*Context)

	// Context is the contexual values supplied to middlewares and handlers
	Context struct {
		Command   string
		Arguments []string
		Session   *discordgo.Session
		Message   *discordgo.MessageCreate
	}

	// ErrorTexts holds strings used when an error occurs
	ErrorTexts struct {
		CommandNotFound string
		NoPermissions   string
	}

	// Permissions holds permissions for a given command in whitelist format
	// Currently in development, only supports RoleIDs at the moment.
	Permissions struct {
		UserIDs []string
		RoleIDs []string
		ChanIDs []string
	}

	// Command specifies the functions for a multiplexed command
	Command interface {
		Settings() CommandSettings
		Init(m *Mux)
		Handle(ctx *Context)
		Done(m *Mux)
		Permissions() Permissions
	}

	// CommandSettings contain command-specific settings the multiplexer should
	// know.
	CommandSettings struct {
		Command string
	}

	//TODO: This probably isn't a great logger implmentation, but I'm running
	//out of ideas
	Logger interface {
		Init(m *Mux)
		Info(message string)
		Warn(message string)
		Err(message string)
		Done(m *Mux)
	}
)

// New initlaizes a new Mux object
func New(prefix string) (*Mux, error) {
	if len(prefix) > 1 {
		return &Mux{}, fmt.Errorf("Prefix %s greater than 1 character", prefix)
	}

	return &Mux{
		Prefix:   prefix,
		Commands: make(map[string]Command),
		logger:   nil,
		errorTexts: ErrorTexts{
			CommandNotFound: "Command not found.",
			NoPermissions:   "You do not have permission to use that command",
		},
	}, nil
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
	/* Ignore if the message is not a regular message */
	if message.Type != discordgo.MessageTypeDefault || len(message.Content) == 0 {
		return
	}

	/* Ignore if the message being handled originated from the bot */
	if message.Author.ID == session.State.User.ID {
		return
	}

	if !strings.HasPrefix(message.Content, m.Prefix) {
		return
	}

	/* Split the message on the space */
	args := strings.Split(message.Content, " ")
	command := args[0][1:]

	handler, ok := m.Commands[command]
	if !ok {
		session.ChannelMessageSend(
			message.ChannelID,
			m.errorTexts.CommandNotFound,
		)
		return
	}

	p := handler.Permissions()
	if len(p.RoleIDs) != 0 {
		member, err := session.GuildMember(message.GuildID, message.Author.ID)
		if err != nil {
			session.ChannelMessageSend(
				message.ChannelID,
				"There was a weird issue. Check Bot log.",
			)
			if m.logger != nil {
				m.logger.Err(
					fmt.Sprintf("Could not find member %q of Guild %q.",
						message.Author.ID, message.GuildID,
					),
				)
			}
			return
		}

		for _, r := range member.Roles {
			if arrayContains(p.RoleIDs, r) {
				go handler.Handle(&Context{
					Command:   command,
					Arguments: args[1:],
					Session:   session,
					Message:   message,
				})
				return
			}
		}

		session.ChannelMessageSend(
			message.ChannelID, m.errorTexts.NoPermissions,
		)
		return
	}

	go handler.Handle(&Context{
		Command:   command,
		Arguments: args[1:],
		Session:   session,
		Message:   message,
	})
}

// ChannelSend is a helper function for easily sending a message to the current
// channel.
func (ctx *Context) ChannelSend(message string) {
	ctx.Session.ChannelMessageSend(ctx.Message.ChannelID, message)
}

// Logger allows a custom middleware logger function to be used
func (m *Mux) Logger(l Logger) {
	l.Init(m)
	m.logger = l
}

func arrayContains(array []string, value string) bool {
	for _, e := range array {
		if e == value {
			return true
		}
	}
	return false
}
