package function

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-redis/redis"
	"net/http"
	"os"
	"sort"
	"time"
)

var redisHost = os.Getenv("REDIS_HOST")

type SessionScore struct{
	SessionId string
	Nickname string
	AccumulatedScore int
	AccumulatedTimeInSeconds int64
	Time time.Time
	LastLevel string
}

type GameSession struct{
	SessionId string
	Player string
}

type Leaderboard struct{
	Sessions []SessionScore
}

type Score struct {
	SessionId string
	Time time.Time
	Level string
	LevelScore int
}

type GameTime struct{
	GameTimeId string
	SessionId string
	Level string
	Type string
	Time      time.Time
}
// This function expects 3 kind of keys stored in redis:
// - `game-<UUID>` keys map to GameSession (SessionId, Player)
// - `score-game-<UUID>` keys map to Score (SessionId, Time, Level, LevelScore)
// - `time-game-<UUID>` keys map to GameTime (GameTimeId, SessionId, Level, Type, Time)
// Based on these 3 keys it builds the Leaderboard (SessionScore[]) -> SessionScore(SessionId, Nickname, Time, AccumulatedScore, AccumulatedTimeInSeconds, LastLevel)
//  It does this, by getting all the game KEYS `game-*`, then for each Key it gets the GameSession and create a SessionScore for each session.
//  Once it has the session it needs to iterate with an LRANGE the `score-game-<UUID>` key for each session to calculate the accumulated score and which was the last level played
//  Then it proceed to iterate with an LRANGE the `time-game-<UUID>` key for each session to calculate the accumulated time in seconds

// Handle an HTTP Request.
func Handle(ctx context.Context, res http.ResponseWriter, req *http.Request) {

	var lead Leaderboard

	client := redis.NewClient(&redis.Options{
		Addr:     redisHost + ":6379",
		Password: "",
		DB:       0,
	})

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

		var score Score
		var accumulatedScore = 0
		var lastActiveTime time.Time
		var lastLevel string
		for _, scoreJson := range sessionScores {
			err := json.Unmarshal([]byte(scoreJson), &score)
			if err != nil {
				http.Error(res, err.Error(), http.StatusBadRequest)
				return
			}
			accumulatedScore += score.LevelScore
			lastActiveTime = score.Time
			lastLevel = score.Level

		}

		var accumulatedTimeInSeconds int64
		sessionTimes, err := client.LRange("time-"+session, 0, -1).Result()
		if err != nil {
			http.Error(res, err.Error(), http.StatusBadRequest)
			return
		}
		var gt GameTime
		var tempTime time.Time
		var currentLevel string
		for _, timeJson := range sessionTimes {
			err := json.Unmarshal([]byte(timeJson), &gt)
			if err != nil {
				http.Error(res, err.Error(), http.StatusBadRequest)
				return
			}

			if gt.Type == "start" {
				tempTime = gt.Time
				currentLevel = gt.Level
			} else if gt.Type == "end" && currentLevel == gt.Level {
				accumulatedTimeInSeconds += gt.Time.UnixMilli() - tempTime.UnixMilli()
			}
		}
		s.SessionId = session
		s.Nickname = gs.Player
		s.AccumulatedScore = accumulatedScore
		s.AccumulatedTimeInSeconds = accumulatedTimeInSeconds
		s.Time = lastActiveTime
		s.LastLevel = lastLevel
		lead.Sessions = append(lead.Sessions, s)
	}

	sort.SliceStable(lead.Sessions, func(i, j int) bool {
		if lead.Sessions[i].AccumulatedScore == lead.Sessions[j].AccumulatedScore{
			return lead.Sessions[i].AccumulatedTimeInSeconds < lead.Sessions[j].AccumulatedTimeInSeconds // returns 1 if i is less than j
		}
		return lead.Sessions[i].AccumulatedScore > lead.Sessions[j].AccumulatedScore // returns 1 if i is greater than j -> returns 0 if they are the same -> returns -1 if j is greater than i
	})

	leaderboard, err := json.Marshal(lead)
	if err != nil {
		http.Error(res, err.Error(), http.StatusBadRequest)
		return
	}

	res.Header().Set("Content-Type", "application/json")
	fmt.Fprintln(res, string(leaderboard) )

}
