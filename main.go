/*
Copyright (c) 2018 ceriath
This Package is part of the "markerbot"
It is licensed under the MIT License
*/

//Package main contains the markerbot for twitch.
package main

import (
	"strconv"
	"strings"

	"code.cerinuts.io/libs/goBlue/archium"
	"code.cerinuts.io/libs/goBlue/log"
	"code.cerinuts.io/libs/goBlue/network"
	"code.cerinuts.io/libs/goBlue/settings"
	"code.cerinuts.io/libs/goBlue/util"
	"code.cerinuts.io/libs/goPurple/gql"
	"code.cerinuts.io/libs/goPurple/irc"
	"code.cerinuts.io/libs/goPurple/twitchapi"
)

const AppName, VersionMajor, VersionMinor, VersionBuild string = "markerbot", "0", "2", "s"
const FullVersion string = AppName + VersionMajor + "." + VersionMinor + VersionBuild

type botSettings struct {
	Host            string                      `json:"host"`
	Port            string                      `json:"port"`
	Oauth           string                      `json:"oauth"`
	ClientID        string                      `json:"clientId"`
	Username        string                      `json:"username"`
	Loglevel        int                         `json:"loglevel"`
	ChannelSettings map[string]*channelSettings `json:"channelSettings"`
	GlobalAdmins    []string                    `json:"globaladmins"`
}

type channelSettings struct {
	Name            string   `json:"name"`
	ID              string   `json:"id"`
	EnableAllMods   bool     `json:"enableAllMods"`
	AuthorizedUsers []string `json:"authorizedUsers"`
}

const botname = "markersbot"
const settingspath = "./settings.json"

var config *botSettings
var ircConn *irc.Connection
var kraken *twitchapi.TwitchKraken
var gqlClient *gql.Client

func main() {
	config = new(botSettings)
	settings.ReadJSONConfig(settingspath, &config)
	log.CurrentLevel = config.Loglevel
	log.PrintToFile = true
	log.Logfilename = "markerbot.log"

	if config.ChannelSettings == nil {
		config.ChannelSettings = make(map[string]*channelSettings)
		settings.WriteJSONConfig(settingspath, &config)
	}

	if config.GlobalAdmins == nil {
		config.GlobalAdmins = make([]string, 0)
		settings.WriteJSONConfig(settingspath, &config)
	}

	kraken = &twitchapi.TwitchKraken{
		ClientID: config.ClientID,
	}

	gqlClient = &gql.Client{
		ClientID: config.ClientID,
		OAuth:    config.Oauth,
	}

	a := archium.ArchiumCore
	cal := new(ChannelListener)
	col := new(ConfigListener)
	dl := new(archium.DebugListener)
	a.Register(cal)
	a.Register(col)
	a.Register(dl)

	ircConn = new(irc.Connection)
	err := ircConn.Connect(config.Host, config.Port)
	if err != nil {
		log.P(err)
	}
	ircConn.Init("oauth:"+config.Oauth, config.Username)
	ircConn.Join(config.Username)
	for k := range config.ChannelSettings {
		ircConn.Join(k)
	}

	ircConn.Wait()
}

//ChannelListener is the listener for userchannels
type ChannelListener struct {
}

//Trigger handles everything on a normal channel
func (cal *ChannelListener) Trigger(ae archium.Event) {
	ircMessage := ae.Data[irc.ArchiumDataIdentifier].(*irc.Message)

	if strings.HasPrefix(ircMessage.Msg, "!markersbot") || strings.HasPrefix(ircMessage.Msg, "!markerbot") {
		if strings.HasPrefix(ircMessage.Msg, "!markersbot leave") || strings.HasPrefix(ircMessage.Msg, "!markerbot leave") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				delete(config.ChannelSettings, ircMessage.Channel)
				settings.WriteJSONConfig(settingspath, &config)
				ircConn.Send("Goodbye.", ircMessage.Channel)
				ircConn.Leave(ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot mods enable") || strings.HasPrefix(ircMessage.Msg, "!markerbot mods enable") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				if config.ChannelSettings[ircMessage.Channel].EnableAllMods {
					ircConn.Send("The moderators of this channel are already authorized to create markers.", ircMessage.Channel)
					return
				}
				config.ChannelSettings[ircMessage.Channel].EnableAllMods = true
				settings.WriteJSONConfig(settingspath, &config)
				ircConn.Send("The moderators of this channel are now authorized to create markers.", ircMessage.Channel)
				return
			}
		}

		if strings.HasPrefix(ircMessage.Msg, "!markersbot mods disable") || strings.HasPrefix(ircMessage.Msg, "!markerbot mods disable") {
			if isBroadcaster(ircMessage.Tags["user-id"], ircMessage.Channel) || isGlobalAdmin(ircMessage.Tags["user-id"]) {
				if !config.ChannelSettings[ircMessage.Channel].EnableAllMods {
					ircConn.Send("The moderators of this channel are already not authorized to create markers.", ircMessage.Channel)
					return
				}
				config.ChannelSettings[ircMessage.Channel].EnableAllMods = false
				settings.WriteJSONConfig(settingspath, &config)
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

//GetTypes returns any twitch privmsg
func (cal *ChannelListener) GetTypes() []string {
	var list []string
	list = append(list, irc.ArchiumPrefix+"*.privmsg")
	return list
}

//ConfigListener listens on the bot's own channel
type ConfigListener struct {
}

//Trigger handles the messages on the bots own channel
func (col *ConfigListener) Trigger(ae archium.Event) {
	ircMessage := ae.Data[irc.ArchiumDataIdentifier].(*irc.Message)
	isAdmin := isGlobalAdmin(ircMessage.Tags["user-id"])

	if strings.HasPrefix(ircMessage.Msg, "!join") {

		if len(ircMessage.Msg) > len("!join ") && !isAdmin {
			split := strings.Split(ircMessage.Msg, " ")
			ircConn.Send("You are not allowed to add "+botname+" to channel "+split[1]+". You can only add it to your own channel by typing !join", ircMessage.Channel)
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
			ircConn.Send("You are not allowed to remove "+botname+" from channel "+split[1]+". You can only remove it from your own channel by typing !leave", ircMessage.Channel)
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

//GetTypes returns all privmsgs on the bots channel
func (col *ConfigListener) GetTypes() []string {
	var list []string
	list = append(list, irc.ArchiumPrefix+strings.ToLower(botname)+".privmsg")
	return list
}

func isGlobalAdmin(userID string) bool {
	for _, admin := range config.GlobalAdmins {
		if admin == userID {
			return true
		}
	}
	return false
}

func isAuthorized(userID, channel string) bool {
	if isGlobalAdmin(userID) || isBroadcaster(userID, channel) {
		return true
	}

	for _, user := range config.ChannelSettings[channel].AuthorizedUsers {
		if user == userID {
			return true
		}
	}
	return false

}

func isBroadcaster(userID, channel string) bool {
	return userID == config.ChannelSettings[channel].ID
}

func getBroadcastID(channelName string) string {
	stream, jsoerr, err := kraken.GetStream(config.ChannelSettings[channelName].ID, "")
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

func handleJoin(channelID, username, sourceChannel string) {
	var err error
	var jsoerr *network.JSONError
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
		channel, jsoerr, err = kraken.GetChannel(channelID)
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
	config.ChannelSettings[toJoin] = &channelSettings{
		Name:            toJoin,
		EnableAllMods:   false,
		AuthorizedUsers: make([]string, 0),
		ID:              id,
	}
	settings.WriteJSONConfig(settingspath, &config)
	ircConn.Send(botname+" added to "+toJoin+".", sourceChannel)
}

func handleLeave(channelID, username, sourceChannel string) {
	var toLeave string
	if username != "" {
		toLeave = username
	} else {
		channel, jsoerr, err := kraken.GetChannel(channelID)
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
	settings.WriteJSONConfig(settingspath, &config)
	ircConn.Send("Goodbye.", toLeave)
	if toLeave != sourceChannel {
		ircConn.Send("Left "+toLeave, sourceChannel)
	}
	ircConn.Leave(toLeave)
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

	for _, uID := range config.ChannelSettings[sourceChannel].AuthorizedUsers {
		if users.Users[0].ID.String() == uID {
			ircConn.Send("User "+users.Users[0].Name+" is already authorized.", sourceChannel)
			return
		}
	}

	config.ChannelSettings[sourceChannel].AuthorizedUsers = append(config.ChannelSettings[sourceChannel].AuthorizedUsers, users.Users[0].ID.String())
	settings.WriteJSONConfig(settingspath, &config)
	ircConn.Send("User "+users.Users[0].Name+" successfully added.", sourceChannel)
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

	for _, uID := range config.ChannelSettings[sourceChannel].AuthorizedUsers {
		if users.Users[0].ID.String() == uID {
			config.ChannelSettings[sourceChannel].AuthorizedUsers = util.RemoveFromStringSlice(config.ChannelSettings[sourceChannel].AuthorizedUsers, users.Users[0].ID.String())
			settings.WriteJSONConfig(settingspath, &config)
			ircConn.Send("User "+users.Users[0].Name+" successfully removed.", sourceChannel)
			return
		}
	}

	ircConn.Send("User "+users.Users[0].Name+" is already unauthorized.", sourceChannel)
}

func handleMarker(msg, username, sourceChannel string) {
	description := strings.TrimPrefix(msg, "!marker")
	currentBroadcastID := getBroadcastID(sourceChannel)

	if len(currentBroadcastID) < 1 {
		ircConn.Send("@"+username+" an error occured for this marker. Is this channel live?", sourceChannel)
		return
	}

	response, err := gqlClient.CreateVideoBookmarkInput(description+" by "+username, currentBroadcastID)
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
	for c := range config.ChannelSettings {
		ircConn.Send(toSend, c)
	}
	ircConn.Send("Sent "+strconv.Itoa(len(config.ChannelSettings))+" messages.", sourceChannel)
}
