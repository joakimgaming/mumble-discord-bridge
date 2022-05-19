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
	prefix := "/"+l.Bridge.BridgeConfig.Command
	if strings.HasPrefix(e.Message, prefix + " getdiscordusers") {
		l.Bridge.DiscordUsersMutex.Lock()
		message := "Current users in discord:\n"
		for userId, user := range l.Bridge.DiscordUsers {
			message += user.username + " → " + userId + "\n"
		}
		l.Bridge.DiscordUsersMutex.Unlock()
		e.Sender.Send(message)
	}
	if strings.HasPrefix(e.Message, prefix + " setdiscorduservolume") {
		command := strings.Split(e.Message, " ")
		if len(command) != 4 {
			e.Sender.Send("Invalid amount of arguments! usage: '" + prefix + " setdiscorduservolume <ID> <VOLUME>'")
			return
		}
		if _, ok := l.Bridge.DiscordUsers[command[2]]; !ok {
			e.Sender.Send("Invalid user! use '" + prefix + " getdiscordusers' to get a list of users")
			return
		}
		// either volume percentage or volume as a float or just an int/number below 200
		exp, err := regexp.Compile(`^((\d|\d\d|1\d\d)(\\.\d+)?|200)%?$`)
		if err != nil {
			fmt.Println("you are bad at writing regex")
			return
		}
		if !exp.MatchString(command[3]) {
			e.Sender.Send("Bad volume value! try a number less than or equal to 200")
		}
		volumepercent, err := strconv.ParseFloat(exp.FindStringSubmatch(command[3])[1], 64)
		if err != nil {
			e.Sender.Send("Invalid volume value, how you manage to get this error is a whole nother question tho")
			return
		}
		l.Bridge.DiscordUserVolumeMutex.Lock()
		l.Bridge.DiscordUserVolume[command[2]] = volumepercent/100
	}
}
