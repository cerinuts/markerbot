package main

import (
	"strings"

	"gitlab.ceriath.net/libs/goBlue/archium"
	"gitlab.ceriath.net/libs/goBlue/log"
	"gitlab.ceriath.net/libs/goBlue/settings"
	"gitlab.ceriath.net/libs/goBlue/util"
	"gitlab.ceriath.net/libs/goPurple/gql"
	"gitlab.ceriath.net/libs/goPurple/irc"
	"gitlab.ceriath.net/libs/goPurple/twitchapi"
)

type Settings struct {
	Host            string                      `json:"host"`
	Port            string                      `json:"port"`
	Oauth           string                      `json:"oauth"`
	ClientId        string                      `json:"clientId"`
	Username        string                      `json:"username"`
	Loglevel        int                         `json:"loglevel"`
	ChannelSettings map[string]*ChannelSettings `json:"channelsettings"`
	GlobalAdmins    []string                    `json:"globaladmins"`
}

type ChannelSettings struct {
	Name            string   `json:"name"`
	Id              string   `json:"id"`
	EnableAllMods   bool     `json:"enableAllMods"`
	AuthorizedUsers []string `json:"authorizedUsers"`
}

const BOTNAME = "markersbot"
const SETTINGSPATH = "./settings.json"

var config *Settings
var ircConn *irc.IrcConnection
var kraken *twitchapi.TwitchKraken
var gqlClient *gql.GQLClient

func main() {
	config = new(Settings)
	settings.ReadJsonConfig(SETTINGSPATH, &config)
	log.CurrentLevel = config.Loglevel
	log.PrintToFile = true
	log.Logfilename = "markerbot.log"

	if config.ChannelSettings == nil {
		config.ChannelSettings = make(map[string]*ChannelSettings)
		settings.WriteJsonConfig(SETTINGSPATH, &config)
	}

	if config.GlobalAdmins == nil {
		config.GlobalAdmins = make([]string, 0)
		settings.WriteJsonConfig(SETTINGSPATH, &config)
	}

	kraken = &twitchapi.TwitchKraken{
		ClientID: config.ClientId,
	}

	gqlClient = &gql.GQLClient{
		ClientId: config.ClientId,
		OAuth:    config.Oauth,
	}

	a := archium.ArchiumCore
	cal := new(ChannelListener)
	col := new(ConfigListener)
	dl := new(archium.ArchiumDebugListener)
	a.Register(cal)
	a.Register(col)
	a.Register(dl)

	ircConn = new(irc.IrcConnection)
	err := ircConn.Connect(config.Host, config.Port)
	if err != nil {
		log.P(err)
	}
	ircConn.Init("oauth:"+config.Oauth, config.Username)
	ircConn.Join(config.Username)
	for k, _ := range config.ChannelSettings {
		ircConn.Join(k)
	}

	ircConn.Wait()
}

type ChannelListener struct {
}

func (cal *ChannelListener) Trigger(ae archium.ArchiumEvent) {
	ircMessage := ae.Data[irc.ArchiumDataIdentifier].(*irc.IrcMessage)

	if strings.HasPrefix(ircMessage.Msg, "!markersbot") || strings.HasPrefix(ircMessage.Msg, "!markerbot") {
		if strings.HasPrefix(ircMessage.Msg, "!markersbot leave") || strings.HasPrefix(ircMessage.Msg, "!markerbot leave") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				delete(config.ChannelSettings, ircMessage.Channel)
				settings.WriteJsonConfig(SETTINGSPATH, &config)
				ircConn.Send("Goodbye.", ircMessage.Channel)
				ircConn.Leave(ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot mods enable") || strings.HasPrefix(ircMessage.Msg, "!markerbot mods enable") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				config.ChannelSettings[ircMessage.Channel].EnableAllMods = true
				settings.WriteJsonConfig(SETTINGSPATH, &config)
				ircConn.Send("The moderators of this channel are now authorized to create markers.", ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot mods disable") || strings.HasPrefix(ircMessage.Msg, "!markerbot mods disable") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				config.ChannelSettings[ircMessage.Channel].EnableAllMods = false
				settings.WriteJsonConfig(SETTINGSPATH, &config)
				ircConn.Send("The moderators of this channel are not authorized to create markers anymore.", ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot add") || strings.HasPrefix(ircMessage.Msg, "!markerbot add") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				split := strings.Split(ircMessage.Msg, " ")
				if len(split) < 3 {
					ircConn.Send("Please provide a username.", ircMessage.Channel)
					return
				}
				username := strings.Split(ircMessage.Msg, " ")[2]

				users, jsoerr, err := kraken.GetUserByName(username)
				if jsoerr != nil {
					log.E(jsoerr.String())
					ircConn.Send("An error occured. Does this user exist?", ircMessage.Channel)
					return
				}

				if err != nil {
					log.E(err)
					ircConn.Send("An error occured. Does this user exist?", ircMessage.Channel)
					return
				}

				if len(users.Users) < 1 {
					ircConn.Send("An error occured. Does this user exist?", ircMessage.Channel)
					return
				}

				config.ChannelSettings[ircMessage.Channel].AuthorizedUsers = append(config.ChannelSettings[ircMessage.Channel].AuthorizedUsers, users.Users[0].ID.String())
				settings.WriteJsonConfig(SETTINGSPATH, &config)
				ircConn.Send("User "+users.Users[0].Name+" successfully added.", ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot remove") || strings.HasPrefix(ircMessage.Msg, "!markerbot remove") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				split := strings.Split(ircMessage.Msg, " ")
				if len(split) < 3 {
					ircConn.Send("Please provide a username.", ircMessage.Channel)
					return
				}
				username := strings.Split(ircMessage.Msg, " ")[2]

				users, jsoerr, err := kraken.GetUserByName(username)
				if jsoerr != nil {
					log.E(jsoerr.String())
					ircConn.Send("An error occured. Does this user exist?", ircMessage.Channel)
					return
				}

				if err != nil {
					log.E(err)
					ircConn.Send("An error occured. Does this user exist?", ircMessage.Channel)
					return
				}

				if len(users.Users) < 1 {
					ircConn.Send("An error occured. Does this user exist?", ircMessage.Channel)
					return
				}

				config.ChannelSettings[ircMessage.Channel].AuthorizedUsers = util.RemoveFromStringSlice(config.ChannelSettings[ircMessage.Channel].AuthorizedUsers, users.Users[0].ID.String())
				settings.WriteJsonConfig(SETTINGSPATH, &config)
				ircConn.Send("User "+users.Users[0].Name+" successfully removed.", ircMessage.Channel)
				return
			}
		}
	}

	if strings.HasPrefix(ircMessage.Msg, "!marker") && !strings.HasPrefix(ircMessage.Msg, "!markersbot") && !strings.HasPrefix(ircMessage.Msg, "!markerbot") {

		if (ircMessage.Tags["mod"] == "1" && config.ChannelSettings[ircMessage.Channel].EnableAllMods) || isAuthorized(ircMessage.Tags["user-id"], ircMessage.Channel) {

			description := strings.TrimPrefix(ircMessage.Msg, "!marker")
			currentBroadcastId := getBroadcastId(ircMessage.Channel)

			if len(currentBroadcastId) < 1 {
				ircConn.Send("@"+ircMessage.Tags["display-name"]+" an error occured for this marker. Is this channel live?", ircMessage.Channel)
				return
			}

			response, err := gqlClient.CreateVideoBookmarkInput(description+" by "+ircMessage.Tags["display-name"], currentBroadcastId)
			if err != nil {
				ircConn.Send("@"+ircMessage.Tags["display-name"]+" an error occured for this marker.", ircMessage.Channel)
				log.E(err)
				return
			}

			if response.Data.CreateVideoBookmark.Error != nil {
				log.E(response.Data.CreateVideoBookmark.Error)
				ircConn.Send("@"+ircMessage.Tags["display-name"]+" an error occured for this marker.", ircMessage.Channel)
				return
			}
			ircConn.Send("@"+ircMessage.Tags["display-name"]+" the marker "+description+" has been added.", ircMessage.Channel)

		}
	}

}

func (cal *ChannelListener) GetTypes() []string {
	var list []string
	list = append(list, irc.ArchiumPrefix+"*.privmsg")
	return list
}

type ConfigListener struct {
}

func (col *ConfigListener) Trigger(ae archium.ArchiumEvent) {
	ircMessage := ae.Data[irc.ArchiumDataIdentifier].(*irc.IrcMessage)

	if strings.HasPrefix(ircMessage.Msg, "!join") {

		toJoin := ""
		id := ""
		isAdmin := isGlobalAdmin(ircMessage.Tags["user-id"])

		if len(ircMessage.Msg) > len("!join ") && !isAdmin {
			split := strings.Split(ircMessage.Msg, " ")
			ircConn.Send("You are not allowed to add "+BOTNAME+" to channel "+split[1]+". You can only add it to your own channel by typing !join", ircMessage.Channel)
			return
		} else if len(ircMessage.Msg) > len("!join ") && isAdmin {
			users, jsoerr, err := kraken.GetUserByName(strings.Split(ircMessage.Msg, " ")[1])
			if err != nil {
				log.E(err)
				ircConn.Send("An error occured.", ircMessage.Channel)
				return
			}

			if jsoerr != nil {
				log.E(jsoerr.String())
				ircConn.Send("An error occured.", ircMessage.Channel)
				return
			}

			if len(users.Users) < 1 {
				ircConn.Send("An error occured. Does the user exist?", ircMessage.Channel)
				return
			}

			toJoin = users.Users[0].Name
			id = users.Users[0].ID.String()

		} else {
			channel, jsoerr, err := kraken.GetChannel(ircMessage.Tags["user-id"])
			if err != nil {
				log.E(err)
				ircConn.Send("An error occured.", ircMessage.Channel)
				return
			}

			if jsoerr != nil {
				log.E(jsoerr.String())
				ircConn.Send("An error occured.", ircMessage.Channel)
				return
			}
			toJoin = channel.Name
			id = channel.ID.String()
		}

		toJoin = strings.ToLower(toJoin)
		if _, ok := config.ChannelSettings[toJoin]; ok {
			ircConn.Send("The bot is already active for this channel.", ircMessage.Channel)
			return
		}

		ircConn.Join(toJoin)
		config.ChannelSettings[toJoin] = &ChannelSettings{
			Name:            toJoin,
			EnableAllMods:   false,
			AuthorizedUsers: make([]string, 0),
			Id:              id,
		}
		settings.WriteJsonConfig(SETTINGSPATH, &config)
		ircConn.Send(BOTNAME+" added to "+toJoin+".", ircMessage.Channel)
	}
}

func (col *ConfigListener) GetTypes() []string {
	var list []string
	list = append(list, irc.ArchiumPrefix+strings.ToLower(BOTNAME)+".privmsg")
	return list
}

func isGlobalAdmin(userid string) bool {
	for _, admin := range config.GlobalAdmins {
		if admin == userid {
			return true
		}
	}
	return false
}

func isAuthorized(userId, channel string) bool {
	if isGlobalAdmin(userId) || isBroadcaster(userId, channel) {
		return true
	}

	for _, user := range config.ChannelSettings[channel].AuthorizedUsers {
		if user == userId {
			return true
		}
	}
	return false

}

func isBroadcaster(userId, channel string) bool {
	if userId == config.ChannelSettings[channel].Id {
		return true
	}
	return false
}

func getBroadcastId(channelName string) string {
	stream, jsoerr, err := kraken.GetStream(config.ChannelSettings[channelName].Id, "")
	if err != nil {
		log.E(err)
		return ""
	}

	if jsoerr != nil {
		log.E(jsoerr.String())
		return ""
	}

	return stream.Stream.ID.String()

}
