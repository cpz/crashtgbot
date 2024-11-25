package main

import (
	"fmt"
	"math/rand"
	"strconv"
	"strings"
	"sync"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Participant struct {
	Name       string
	Multiplier float64
}

type Game struct {
	ownerID      int64
	balance      float64
	participants map[int64]Participant
	userBalances map[int64]float64 // Stores individual user balances
	isActive     bool
	mutex        sync.Mutex
}

var game = Game{
	ownerID:      1337,
	balance:      200, // Initial game balance
	participants: make(map[int64]Participant),
	userBalances: make(map[int64]float64),
}

func main() {
	bot, err := tgbotapi.NewBotAPI("1337:AAEA-0_1hz4iOjfd7pjRM8BCO9ij3MVCnmQ")
	if err != nil {
		panic(err)
	}
	bot.Debug = true

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := bot.GetUpdatesChan(u)

	for update := range updates {
		if update.Message == nil {
			continue
		}

		// Handle commands
		switch update.Message.Command() {
		case "start":
			handleStart(bot, update.Message)
		case "play":
			handlePlay(bot, update.Message)
		case "join":
			handleJoin(bot, update.Message)
		case "balance":
			handleBalance(bot, update.Message)
		case "help":
			handleHelp(bot, update.Message)
		}
	}
}

func handleStart(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	if game.ownerID == 0 {
		game.ownerID = msg.From.ID
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "You are now the game owner! Use /play to start the game."))
	} else {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "A game owner is already set. Only they can start the game."))
	}
}

func handlePlay(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	if msg.From.ID != game.ownerID {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Only the game owner can start the game!"))
		return
	}

	if game.isActive {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Game is already active."))
		return
	}

	if game.balance <= 0 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "The bot is out of balance. Game over."))
		return
	}

	game.isActive = true
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "The game has started! Participants, type /join <multiplier> to participate."))

	go runGame(bot, msg.Chat.ID)
}

func handleJoin(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	if !game.isActive {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "No active game. Please wait for the owner to start a new game."))
		return
	}

	// Extract the multiplier from the command
	args := strings.Fields(msg.Text)
	if len(args) != 2 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Usage: /join <multiplier>. Example: /join 3.5"))
		return
	}

	multiplier, err := strconv.ParseFloat(args[1], 64)
	if err != nil || multiplier <= 0 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Invalid multiplier. Please specify a positive number."))
		return
	}

	if _, exists := game.participants[msg.From.ID]; exists {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "You have already joined the game."))
		return
	}

	game.participants[msg.From.ID] = Participant{
		Name:       msg.From.FirstName,
		Multiplier: multiplier,
	}
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("%s joined the game with multiplier %.2f!", msg.From.FirstName, multiplier)))
}

func handleBalance(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	game.mutex.Lock()
	defer game.mutex.Unlock()

	args := strings.Fields(msg.Text)
	if len(args) != 2 {
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Usage: /balance <me|game|all>."))
		return
	}

	switch args[1] {
	case "me":
		balance := game.userBalances[msg.From.ID]
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Your balance: %.2f", balance)))
	case "game":
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("Game balance: %.2f", game.balance)))
	case "all":
		if msg.From.ID == game.ownerID {
			message := fmt.Sprintf("Game balance: %.2f\n", game.balance)
			for userID, balance := range game.userBalances {
				// Create a clickable user mention using Markdown format
				userMention := fmt.Sprintf("[User](tg://user?id=%d)", userID)
				message += fmt.Sprintf("%s: %.2f\n", userMention, balance)
			}
			// Create message with Markdown parsing mode
			responseMsg := tgbotapi.NewMessage(msg.Chat.ID, message)
			responseMsg.ParseMode = "Markdown"
			bot.Send(responseMsg)
		} else {
			bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Only the game owner can see all balances!"))
		}
	default:
		bot.Send(tgbotapi.NewMessage(msg.Chat.ID, "Invalid option. Use /balance <me|game|all>."))
	}
}

func handleHelp(bot *tgbotapi.BotAPI, msg *tgbotapi.Message) {
	helpText := `
Welcome to the Neverlose.CRASH Game Bot! Here are the available commands:

/start - Claim ownership of the bot (first user only).
/play - Start a new game (owner only).
/join <multiplier> - Join the game with a target multiplier. Example: /join 3.
/balance <me|game|all> - Check balances:
    me - Your personal balance.
    game - The bot's game balance.
    all - All balances (game and users).
/help - Show this help message.

Game Rules:
1. The owner starts the game with /play.
2. Participants join with /join <multiplier>.
3. If the game crashes before your multiplier, you lose.
4. If the game reaches your multiplier, you win.
5. The bot's balance decreases when payouts are made. If balance is 0, the game ends.
`
	bot.Send(tgbotapi.NewMessage(msg.Chat.ID, helpText))
}

func runGame(bot *tgbotapi.BotAPI, chatID int64) {
	time.Sleep(2 * time.Minute) // Simulate game running

	game.mutex.Lock()
	defer game.mutex.Unlock()

	if len(game.participants) == 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "No participants joined. Game canceled."))
		game.isActive = false
		return
	}

	// Simulate crash multiplier
	rand.Seed(time.Now().UnixNano())
	crashMultiplier := rand.Float64()*9 + 1 // Random multiplier between 1 and 9

	// Determine winners and losers
	winners := []string{}
	totalPayout := 0.0

	for userID, participant := range game.participants {
		if participant.Multiplier <= crashMultiplier {
			// User wins
			payout := participant.Multiplier
			game.userBalances[userID] += payout
			game.balance -= payout
			totalPayout += payout
			winners = append(winners, fmt.Sprintf("%s (%.2fx)", participant.Name, participant.Multiplier))
		} else {
			bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("%s lost (target: %.2fx, crash: %.2fx)", participant.Name, participant.Multiplier, crashMultiplier)))
		}
	}

	// Announce results
	if len(winners) > 0 {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Game crashed at %.2fx! Winners: %v", crashMultiplier, winners)))
	} else {
		bot.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("Game crashed at %.2fx! No winners this round.", crashMultiplier)))
	}

	if game.balance <= 0 {
		bot.Send(tgbotapi.NewMessage(chatID, "Game over. The bot is out of balance."))
		game.isActive = false
		return
	}

	// Reset participants for the next game
	game.participants = make(map[int64]Participant)
	game.isActive = false
}
