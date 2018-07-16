package main

import (
	"strconv"
	"strings"

	"gitlab.ceriath.net/libs/goBlue/archium"
	"gitlab.ceriath.net/libs/goBlue/log"
	"gitlab.ceriath.net/libs/goBlue/network"
	"gitlab.ceriath.net/libs/goBlue/settings"
	"gitlab.ceriath.net/libs/goBlue/util"
	"gitlab.ceriath.net/libs/goPurple/gql"
	"gitlab.ceriath.net/libs/goPurple/irc"
	"gitlab.ceriath.net/libs/goPurple/twitchapi"
)

const AppName, VersionMajor, VersionMinor, VersionBuild string = "markerbot", "0", "1", "s"
const FullVersion string = AppName + VersionMajor + "." + VersionMinor + VersionBuild

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
				handleAddUser(ircMessage.Msg, ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot remove") || strings.HasPrefix(ircMessage.Msg, "!markerbot remove") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				handleRemoveUser(ircMessage.Msg, ircMessage.Channel)
				return
			}
		}
	}

	if strings.HasPrefix(ircMessage.Msg, "!marker") && !strings.HasPrefix(ircMessage.Msg, "!markersbot") && !strings.HasPrefix(ircMessage.Msg, "!markerbot") {
		if (ircMessage.Tags["mod"] == "1" && config.ChannelSettings[ircMessage.Channel].EnableAllMods) || isAuthorized(ircMessage.Tags["user-id"], ircMessage.Channel) {
			handleMarker(ircMessage.Msg, ircMessage.Tags["display-name"], ircMessage.Channel)
			return
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
	isAdmin := isGlobalAdmin(ircMessage.Tags["user-id"])

	if strings.HasPrefix(ircMessage.Msg, "!join") {

		if len(ircMessage.Msg) > len("!join ") && !isAdmin {
			split := strings.Split(ircMessage.Msg, " ")
			ircConn.Send("You are not allowed to add "+BOTNAME+" to channel "+split[1]+". You can only add it to your own channel by typing !join", ircMessage.Channel)
			return
		} else if len(ircMessage.Msg) > len("!join ") && isAdmin {
			handleJoin("", strings.Split(ircMessage.Msg, " ")[1], ircMessage.Channel)
			return
		} else {
			handleJoin(ircMessage.Tags["user-id"], "", ircMessage.Channel)
			return
		}
	}

	if strings.HasPrefix(ircMessage.Msg, "!leave") {
		if len(ircMessage.Msg) > len("!leave ") && !isAdmin {
			split := strings.Split(ircMessage.Msg, " ")
			ircConn.Send("You are not allowed to remove "+BOTNAME+" from channel "+split[1]+". You can only remove it from your own channel by typing !leave", ircMessage.Channel)
			return
		} else if len(ircMessage.Msg) > len("!leave ") && isAdmin {
			handleLeave("", strings.Split(ircMessage.Msg, " ")[1], ircMessage.Channel)
			return
		} else {
			handleLeave(ircMessage.Tags["user-id"], "", ircMessage.Channel)
			return
		}
	}

	if strings.HasPrefix(ircMessage.Msg, "!info") && isAdmin {
		handleInfo(ircMessage.Channel)
		return
	}

	if strings.HasPrefix(ircMessage.Msg, "!broadcast ") && isAdmin {
		handleBroadcast(ircMessage.Msg, ircMessage.Channel)
		return
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

//handlers

func handleJoin(channelId, username, sourceChannel string) {
	var err error
	var jsoerr *network.JsonError
	var toJoin string
	var id string

	if username != "" {
		var users *twitchapi.Users
		users, jsoerr, err = kraken.GetUserByName(username)
		if users != nil {
			toJoin = users.Users[0].Name
			id = users.Users[0].ID.String()
		}
	} else {
		var channel *twitchapi.Channel
		channel, jsoerr, err = kraken.GetChannel(channelId)
		if channel != nil {
			toJoin = channel.Name
			id = channel.ID.String()
		}
	}
	if err != nil {
		log.E(err)
		ircConn.Send("An error occured.", sourceChannel)
		return
	}

	if jsoerr != nil {
		log.E(jsoerr.String())
		ircConn.Send("An error occured.", sourceChannel)
		return
	}

	toJoin = strings.ToLower(toJoin)
	if _, ok := config.ChannelSettings[toJoin]; ok {
		ircConn.Send("The bot is already active for this channel.", sourceChannel)
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
	ircConn.Send(BOTNAME+" added to "+toJoin+".", sourceChannel)
}

func handleLeave(channelId, username, sourceChannel string) {
	var toLeave string
	if username != "" {
		toLeave = username
	} else {
		channel, jsoerr, err := kraken.GetChannel(channelId)
		if err != nil {
			log.E(err)
			ircConn.Send("An error occured.", sourceChannel)
			return
		}

		if jsoerr != nil {
			log.E(jsoerr.String())
			ircConn.Send("An error occured.", sourceChannel)
			return
		}
		toLeave = channel.Name
	}
	delete(config.ChannelSettings, toLeave)
	settings.WriteJsonConfig(SETTINGSPATH, &config)
	ircConn.Send("Goodbye.", toLeave)
	ircConn.Leave(toLeave)
	return
}

func handleAddUser(msg, sourceChannel string) {
	split := strings.Split(msg, " ")
	if len(split) < 3 {
		ircConn.Send("Please provide a username.", sourceChannel)
		return
	}
	username := strings.Split(msg, " ")[2]

	users, jsoerr, err := kraken.GetUserByName(username)
	if jsoerr != nil {
		log.E(jsoerr.String())
		ircConn.Send("An error occured. Does this user exist?", sourceChannel)
		return
	}

	if err != nil {
		log.E(err)
		ircConn.Send("An error occured. Does this user exist?", sourceChannel)
		return
	}

	if len(users.Users) < 1 {
		ircConn.Send("An error occured. Does this user exist?", sourceChannel)
		return
	}

	config.ChannelSettings[sourceChannel].AuthorizedUsers = append(config.ChannelSettings[sourceChannel].AuthorizedUsers, users.Users[0].ID.String())
	settings.WriteJsonConfig(SETTINGSPATH, &config)
	ircConn.Send("User "+users.Users[0].Name+" successfully added.", sourceChannel)
	return
}

func handleRemoveUser(msg, sourceChannel string) {
	split := strings.Split(msg, " ")
	if len(split) < 3 {
		ircConn.Send("Please provide a username.", sourceChannel)
		return
	}
	username := strings.Split(msg, " ")[2]

	users, jsoerr, err := kraken.GetUserByName(username)
	if jsoerr != nil {
		log.E(jsoerr.String())
		ircConn.Send("An error occured. Does this user exist?", sourceChannel)
		return
	}

	if err != nil {
		log.E(err)
		ircConn.Send("An error occured. Does this user exist?", sourceChannel)
		return
	}

	if len(users.Users) < 1 {
		ircConn.Send("An error occured. Does this user exist?", sourceChannel)
		return
	}

	config.ChannelSettings[sourceChannel].AuthorizedUsers = util.RemoveFromStringSlice(config.ChannelSettings[sourceChannel].AuthorizedUsers, users.Users[0].ID.String())
	settings.WriteJsonConfig(SETTINGSPATH, &config)
	ircConn.Send("User "+users.Users[0].Name+" successfully removed.", sourceChannel)
	return
}

func handleMarker(msg, username, sourceChannel string) {
	description := strings.TrimPrefix(msg, "!marker")
	currentBroadcastId := getBroadcastId(sourceChannel)

	if len(currentBroadcastId) < 1 {
		ircConn.Send("@"+username+" an error occured for this marker. Is this channel live?", sourceChannel)
		return
	}

	response, err := gqlClient.CreateVideoBookmarkInput(description+" by "+username, currentBroadcastId)
	if err != nil {
		ircConn.Send("@"+username+" an error occured for this marker.", sourceChannel)
		log.E(err)
		return
	}

	if response.Data.CreateVideoBookmark.Error != nil {
		log.E(response.Data.CreateVideoBookmark.Error)
		ircConn.Send("@"+username+" an error occured for this marker.", sourceChannel)
		return
	}
	ircConn.Send("@"+username+" the marker "+description+" has been added.", sourceChannel)
}

func handleInfo(sourceChannel string) {
	ircConn.Send(FullVersion+" - "+twitchapi.FullVersion+" - "+irc.FullVersion+" - "+archium.FullVersion+" - "+log.FullVersion+" - "+
		util.FullVersion+" - "+network.FullVersion+" - "+settings.FullVersion, sourceChannel)
}

func handleBroadcast(msg, sourceChannel string) {
	toSend := strings.SplitN(msg, " ", 2)[1]
	for c, _ := range config.ChannelSettings {
		ircConn.Send(toSend, c)
	}
	ircConn.Send("Sent "+strconv.Itoa(len(config.ChannelSettings))+" messages.", sourceChannel)
}
