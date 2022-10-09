package bridge

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/stieneee/gumble/gumble"
)

// MumbleListener Handle mumble events
type MumbleListener struct {
	Bridge *BridgeState
}

func (l *MumbleListener) updateUsers() {
	l.Bridge.MumbleUsersMutex.Lock()
	l.Bridge.MumbleUsers = make(map[string]bool)
	for _, user := range l.Bridge.MumbleClient.Self.Channel.Users {
		//note, this might be too slow for really really big channels?
		//event listeners block while processing
		//also probably bad to rebuild the set every user change.
		if user.Name != l.Bridge.MumbleClient.Self.Name {
			l.Bridge.MumbleUsers[user.Name] = true
		}
	}
	promMumbleUsers.Set(float64(len(l.Bridge.MumbleUsers)))
	l.Bridge.MumbleUsersMutex.Unlock()

}

func (l *MumbleListener) MumbleConnect(e *gumble.ConnectEvent) {
	//join specified channel
	startingChannel := e.Client.Channels.Find(l.Bridge.BridgeConfig.MumbleChannel...)
	if startingChannel != nil {
		e.Client.Self.Move(startingChannel)
	}

	// l.updateUsers() // patch below

	// This is an ugly patch Mumble Client state is slow to update
	time.AfterFunc(5*time.Second, func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Printf("Failed to mumble user list %v \n", r)
			}
		}()
		l.updateUsers()
	})
}

func (l *MumbleListener) MumbleUserChange(e *gumble.UserChangeEvent) {
	l.updateUsers()

	if e.Type.Has(gumble.UserChangeConnected) {

		log.Println("User connected to mumble " + e.User.Name)

		if !l.Bridge.BridgeConfig.MumbleDisableText {
			e.User.Send("Mumble-Discord-Bridge " + l.Bridge.BridgeConfig.Version)

			// Tell the user who is connected to discord
			l.Bridge.DiscordUsersMutex.Lock()
			if len(l.Bridge.DiscordUsers) == 0 {
				e.User.Send("No users connected to Discord")
			} else {
				s := "Connected to Discord: "

				arr := []string{}
				for u := range l.Bridge.DiscordUsers {
					arr = append(arr, l.Bridge.DiscordUsers[u].username)
				}

				s = s + strings.Join(arr[:], ",")

				e.User.Send(s)
			}
			l.Bridge.DiscordUsersMutex.Unlock()

		}

		// Send discord a notice
		l.Bridge.discordSendMessageAll(e.User.Name + " has joined mumble")
	}

	if e.Type.Has(gumble.UserChangeDisconnected) {
		l.Bridge.discordSendMessageAll(e.User.Name + " has left mumble")
		log.Println("User disconnected from mumble " + e.User.Name)
	}
}

func (l *MumbleListener) MumbleTextMessage(e *gumble.TextMessageEvent) {
	if e.Sender == nil {
		return
	}
	prefix := "/" //+ l.Bridge.BridgeConfig.Command <- I don't know what this is supposed to mean?
	if strings.HasPrefix(e.Message, prefix+"help") {
		e.Sender.Send("<br/>/volume (ID) (VOLUME) - change volume on a discord user<br/>/users - shows discord users in channel<br/>" +
			"/mute (ID) - mutes a person in discord<br/>/unmute (ID) - unmutes a person in discord<br/>" +
			"/channels - shows all channels on the discord server<br/>/changechannel (ID) - switch discord channel")
		return
	}

	if strings.HasPrefix(e.Message, prefix+"users") {
		l.Bridge.DiscordUsersMutex.Lock()
		message := "Current users in discord:<br/>"
		for userId, user := range l.Bridge.DiscordUsers {
			message += user.username + " → " + userId + "<br/>"
		}
		l.Bridge.DiscordUsersMutex.Unlock()
		e.Sender.Send(message)
	}

	if strings.HasPrefix(e.Message, prefix+"volume") {
		command := strings.Split(e.Message, " ")
		if len(command) != 3 {
			e.Sender.Send("Invalid amount of arguments! usage: '" + prefix + "volume (ID) (VOLUME)'")
			return
		}
		if _, ok := l.Bridge.DiscordUsers[command[1]]; !ok {
			e.Sender.Send("Invalid user! use '" + prefix + "users' to get a list of users")
			return
		}
		// either volume percentage or volume as a float or just an int/number below 200
		exp, err := regexp.Compile(`^((\d|\d\d|1\d\d)(\\.\d+)?|200)%?$`)
		if err != nil {
			fmt.Println("you are bad at writing regex")
			return
		}
		if !exp.MatchString(command[2]) {
			e.Sender.Send("Bad volume value! try a number less than or equal to 200")
			return
		}
		volumepercent, err := strconv.ParseFloat(exp.FindStringSubmatch(command[2])[0], 64)
		if err != nil {
			e.Sender.Send("Invalid volume value, how you manage to get this error is a whole nother question tho")
			return
		}
		l.Bridge.DiscordUserVolumeMutex.Lock()
		l.Bridge.DiscordUserVolume[command[1]] = volumepercent / 100
		l.Bridge.DiscordUserVolumeMutex.Unlock()
		e.Sender.Send("Volume changed for " + command[1])
	}

	if strings.HasPrefix(e.Message, prefix+"mute") {
		command := strings.Split(e.Message, " ")
		if len(command) != 2 {
			e.Sender.Send("Invalid amount of arguments! usage: '" + prefix + "mute (ID)'")
			return
		}
		if _, ok := l.Bridge.DiscordUsers[command[1]]; !ok {
			e.Sender.Send("Invalid user! use '" + prefix + "users' to get a list of users")
			return
		}
		l.Bridge.DiscordUserVolumeMutex.Lock()
		l.Bridge.DiscordUserVolume[command[1]] = 0
		l.Bridge.DiscordUserVolumeMutex.Unlock()
		e.Sender.Send("Muted " + command[1])
	}

	if strings.HasPrefix(e.Message, prefix+"unmute") {
		command := strings.Split(e.Message, " ")
		if len(command) != 2 {
			e.Sender.Send("Invalid amount of arguments! usage: '" + prefix + "unmute (ID)'")
			return
		}
		if _, ok := l.Bridge.DiscordUsers[command[1]]; !ok {
			e.Sender.Send("Invalid user! use '" + prefix + "users' to get a list of users")
			return
		}
		l.Bridge.DiscordUserVolumeMutex.Lock()
		l.Bridge.DiscordUserVolume[command[1]] = 1
		l.Bridge.DiscordUserVolumeMutex.Unlock()
		e.Sender.Send("Unmuted " + command[1])
	}

	if strings.HasPrefix(e.Message, prefix+"changechannel") {
		command := strings.Split(e.Message, " ")
		if len(command) != 2 {
			e.Sender.Send("Invalid amount of arguments! usage: '" + prefix + "changechannel (ID)'")
			return
		}
		l.Bridge.DiscordChannelID = command[1]
		l.Bridge.BridgeDie <- true
		go l.Bridge.StartBridge()
	}
	if strings.HasPrefix(e.Message, prefix+"channels") {
		l.Bridge.DiscordChannels(l.Bridge.DiscordSession)
		e.Sender.Send(l.Bridge.messagechannel)
	}
}
