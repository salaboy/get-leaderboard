package function

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"net/http"
	"os"
	"sort"
)

type SessionScore struct{
	SessionId string
	Nickname string
	AccumulatedScore int
	LastLevel string
	Selected bool
}

type Leaderboard struct{
	Sessions []SessionScore
}

type GameSession struct{
	SessionId string
	Player string
}



type GameScore struct {
	SessionId string
	Level string
	LevelScore int
}

// This function expects 3 kind of keys stored in redis:
// - `game-<UUID>` keys map to GameSession (SessionId, Player)
// - `score-game-<UUID>` keys map to Score (SessionId, Time, Level, LevelScore)
// - `time-game-<UUID>` keys map to GameTime (GameTimeId, SessionId, Level, Type, Time)
// Based on these 3 keys it builds the Leaderboard (SessionScore[]) -> SessionScore(SessionId, Nickname, Time, AccumulatedScore, AccumulatedTimeInSeconds, LastLevel)
//  It does this, by getting all the game KEYS `game-*`, then for each Key it gets the GameSession and create a SessionScore for each session.
//  Once it has the session it needs to iterate with an LRANGE the `score-game-<UUID>` key for each session to calculate the accumulated score and which was the last level played
//  Then it proceeds to iterate with an LRANGE the `time-game-<UUID>` key for each session to calculate the accumulated time in seconds

var redisHost = os.Getenv("REDIS_HOST")// This should include the port which is most of the time 6379
var redisPassword = os.Getenv("REDIS_PASSWORD")
var redisTLSEnabled = os.Getenv("REDIS_TLS")
var redisTLSEnabledFlag = false
// Handle an HTTP Request.
func Handle(ctx context.Context, res http.ResponseWriter, req *http.Request) {

	var lead Leaderboard
	var nicknameFilter bool
	var frozenLeaderboardDetected bool
	nickname, _ := req.URL.Query()["nickname"]
	if len(nickname) == 1 {
		nicknameFilter = true
	}

	if redisTLSEnabled != "" && redisTLSEnabled != "false" {
		redisTLSEnabledFlag = true
	}
	var client *redis.Client

	if !redisTLSEnabledFlag {
		client = redis.NewClient(&redis.Options{
			Addr:     redisHost,
			Password: redisPassword,
			DB:       0,
		})
	} else {
		client = redis.NewClient(&redis.Options{
			Addr:     redisHost,
			Password: redisPassword,
			DB:       0,
			TLSConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
			},
		})
	}

	// First check if the leaderboard is frozen
	frozenLeaderboard, err := client.Get("frozen").Result()
	if err != nil {
		frozenLeaderboardDetected = false
	}
	if frozenLeaderboard != "" {
		fmt.Println("Frozen Leaderboard: " + frozenLeaderboard)
		frozenLeaderboardDetected = true
		res.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(res, frozenLeaderboard )
		return
	}

	if !frozenLeaderboardDetected {
		fmt.Println("Live Leaderboard being calculated!")
		// First get all the game sessions `game-` using keys, then get the game session
		// -> Then per session key Lrange each `score-game-` and accumulate score
		// -> Then per session key Lrange each `time-game-` and accumulate time

		gameSessions, err := client.Keys("game-*").Result()
		for _, session := range gameSessions {
			var s SessionScore

			var gs GameSession
			gameSession, err := client.Get(session).Result()
			if err != nil {
				http.Error(res, err.Error(), http.StatusBadRequest)
				return
			}
			err = json.Unmarshal([]byte(gameSession), &gs)
			if err != nil {
				http.Error(res, err.Error(), http.StatusBadRequest)
				return
			}

			sessionScores, err := client.LRange("score-"+session, 0, -1).Result()
			if err != nil {
				http.Error(res, err.Error(), http.StatusBadRequest)
				return
			}

			var gameScore GameScore
			var accumulatedScore = 0
			var lastLevel string
			for _, scoreJson := range sessionScores {
				err := json.Unmarshal([]byte(scoreJson), &gameScore)
				if err != nil {
					http.Error(res, err.Error(), http.StatusBadRequest)
					return
				}
				accumulatedScore += gameScore.LevelScore
				lastLevel = gameScore.Level

			}

			s.SessionId = session
			s.Nickname = gs.Player
			s.AccumulatedScore = accumulatedScore
			s.LastLevel = lastLevel
			if nicknameFilter {
				if s.Nickname == nickname[0] {
					s.Selected = true
				}
			}
			lead.Sessions = append(lead.Sessions, s)
		}

		sort.SliceStable(lead.Sessions, func(i, j int) bool {
			return lead.Sessions[i].AccumulatedScore > lead.Sessions[j].AccumulatedScore // returns 1 if i is greater than j -> returns 0 if they are the same -> returns -1 if j is greater than i
		})

		leaderboard, err := json.Marshal(lead)
		if err != nil {
			http.Error(res, err.Error(), http.StatusBadRequest)
			return
		}

		res.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(res, string(leaderboard))
	}

}
