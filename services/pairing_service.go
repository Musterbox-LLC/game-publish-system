package services

import (
	"encoding/json"
	"errors"
	"game-publish-system/models"
	"log"
	"math/rand"
	"sort"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PairingService handles match pairing logic
type PairingService struct {
	DB *gorm.DB
}

func NewPairingService(db *gorm.DB) *PairingService {
	return &PairingService{DB: db}
}

// Pair represents a single pairing between players/teams
type Pair struct {
	Player1ID   string `json:"player1_id"`
	Player1Name string `json:"player1_name"`
	Player2ID   string `json:"player2_id"`
	Player2Name string `json:"player2_name"`
	MatchNumber int    `json:"match_number"`
	TableNumber int    `json:"table_number,omitempty"`
	RoundNumber int    `json:"round_number,omitempty"`
}

// PairingRequest represents a request to generate pairings
type PairingRequest struct {
	MatchID       string `json:"match_id" validate:"required"`
	PairingType   string `json:"pairing_type" validate:"oneof=AUTO MANUAL HYBRID"`
	SeedingMethod string `json:"seeding_method" validate:"oneof=RANDOM RANK_BASED SKILL_BASED CUSTOM"`
	CustomPairs   []Pair `json:"custom_pairs,omitempty"`
	ForceRegenerate bool `json:"force_regenerate"`
}

// PairingResponse represents the response from pairing generation
type PairingResponse struct {
	MatchID      string           `json:"match_id"`
	PairingID    string           `json:"pairing_id"`
	Status       string           `json:"status"`
	Pairs        []Pair           `json:"pairs"`
	TotalPairs   int              `json:"total_pairs"`
	ProposedAt   time.Time        `json:"proposed_at"`
	CanEdit      bool             `json:"can_edit"`
	CanApprove   bool             `json:"can_approve"`
	CanPublish   bool             `json:"can_publish"`
	Metadata     map[string]interface{} `json:"metadata"`
}

// GeneratePairings handles the pairing request
func (ps *PairingService) GeneratePairings(c *fiber.Ctx) error {
	var req PairingRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}

	// Get user ID from context (assuming middleware sets it)
	userID := c.Locals("user_id").(string)
	
	// Fetch match details with batch information
	var match models.TournamentMatch
	if err := ps.DB.First(&match, "id = ?", req.MatchID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "match not found"})
		}
		log.Printf("DB Error fetching match %s: %v", req.MatchID, err)
		return c.Status(500).JSON(fiber.Map{"error": "database error"})
	}

	// Fetch tournament to get subscribers
	var tournament models.Tournament
	if err := ps.DB.First(&tournament, "id = ?", func() string {
		// Get tournament ID through batch
		var batch models.TournamentBatch
		if err := ps.DB.First(&batch, "id = ?", match.BatchID).Error; err != nil {
			return ""
		}
		return batch.TournamentID
	}()).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch tournament"})
	}

	// Get eligible players for this tournament
	players, err := ps.getEligiblePlayerList(tournament.ID)
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch eligible players"})
	}

	if len(players) < 2 {
		return c.Status(400).JSON(fiber.Map{"error": "not enough players for pairing"})
	}

	// Generate pairings based on match type and seeding method
	var pairs []Pair
	var metadata map[string]interface{}
	
	switch req.PairingType {
	case "AUTO":
		pairs, metadata, err = ps.generateAutoPairings(match, players, req.SeedingMethod)
	case "MANUAL":
		if len(req.CustomPairs) == 0 {
			return c.Status(400).JSON(fiber.Map{"error": "custom pairs required for manual pairing"})
		}
		pairs = req.CustomPairs
		metadata = map[string]interface{}{
			"pairing_type":   "MANUAL",
			"seeding_method": req.SeedingMethod,
			"custom":         true,
		}
	case "HYBRID":
		// Start with auto pairings, allow manual adjustments
		pairs, metadata, err = ps.generateAutoPairings(match, players, req.SeedingMethod)
		if err == nil && len(req.CustomPairs) > 0 {
			// Apply manual overrides
			pairs = ps.applyManualOverrides(pairs, req.CustomPairs)
			metadata["hybrid"] = true
			metadata["manual_overrides"] = len(req.CustomPairs)
		}
	}
	
	if err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to generate pairings", "details": err.Error()})
	}

	// Create pairing record
	pairingID := uuid.NewString()
	pairsJSON, _ := json.Marshal(pairs)
	metadataJSON, _ := json.Marshal(metadata)

	// Get tournament ID through batch
	var batch models.TournamentBatch
	if err := ps.DB.First(&batch, "id = ?", match.BatchID).Error; err != nil {
		return c.Status(500).JSON(fiber.Map{"error": "failed to fetch batch"})
	}

	pairing := models.MatchPairing{
		ID:           pairingID,
		MatchID:      match.ID,
		TournamentID: batch.TournamentID,
		BatchID:      match.BatchID,
		PairingType:  req.PairingType,
		SeedingMethod: req.SeedingMethod,
		AlgorithmUsed: match.MatchType,
		PairsJSON:    string(pairsJSON),
		MetadataJSON: string(metadataJSON),
		Status:       "proposed",
		ProposedBy:   userID,
		ProposedAt:   time.Now(),
	}

	if err := ps.DB.Create(&pairing).Error; err != nil {
		log.Printf("DB Error creating pairing: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to save pairing"})
	}

	// Update match with current pairing ID
	ps.DB.Model(&match).Update("current_pairing_id", pairingID)

	response := PairingResponse{
		MatchID:    match.ID,
		PairingID:  pairingID,
		Status:     "proposed",
		Pairs:      pairs,
		TotalPairs: len(pairs),
		ProposedAt: pairing.ProposedAt,
		CanEdit:    true,
		CanApprove: true,
		CanPublish: false,
		Metadata:   metadata,
	}

	return c.Status(200).JSON(response)
}

// getEligiblePlayerList fetches eligible players for a tournament
func (ps *PairingService) getEligiblePlayerList(tournamentID string) ([]models.TournamentSubscription, error) {
	var players []models.TournamentSubscription
	
	query := ps.DB.Where("tournament_id = ? AND payment_status IN ('paid', 'pending')", tournamentID).
		Order("joined_at ASC")
	
	if err := query.Find(&players).Error; err != nil {
		return nil, err
	}
	return players, nil
}

// generateAutoPairings generates automatic pairings based on match type
func (ps *PairingService) generateAutoPairings(match models.TournamentMatch, 
	players []models.TournamentSubscription, seedingMethod string) ([]Pair, map[string]interface{}, error) {
	
	// Get tournament ID through batch
	var batch models.TournamentBatch
	if err := ps.DB.First(&batch, "id = ?", match.BatchID).Error; err != nil {
		return nil, nil, err
	}
	
	// Get player seeding data if available
	var seedings []models.PlayerSeeding
	ps.DB.Where("tournament_id = ?", batch.TournamentID).Find(&seedings)
	
	// Create a map of player seedings for quick lookup
	seedingMap := make(map[string]models.PlayerSeeding)
	for _, seeding := range seedings {
		seedingMap[seeding.UserID] = seeding
	}
	
	// Sort players based on seeding method
	sortedPlayers := ps.sortPlayersBySeeding(players, seedingMap, seedingMethod)
	
	// Generate pairs based on match type
	var pairs []Pair
	var metadata map[string]interface{}
	
	switch match.MatchType {
	case "SINGLE_ELIMINATION_1V1", "DOUBLE_ELIMINATION_1V1":
		pairs, metadata = ps.generateEliminationPairs(sortedPlayers, match.MatchType)
	case "ROUND_ROBIN_1V1":
		pairs, metadata = ps.generateRoundRobinPairs(sortedPlayers)
	case "LEADERBOARD_CHALLENGE":
		pairs, metadata = ps.generateLeaderboardPairs(sortedPlayers)
	case "SWISS_SYSTEM":
		pairs, metadata = ps.generateSwissPairs(sortedPlayers, 1) // First round
	default:
		// Default to simple pairing
		pairs, metadata = ps.generateSimplePairs(sortedPlayers)
	}
	
	metadata["seeding_method"] = seedingMethod
	metadata["match_type"] = match.MatchType
	metadata["player_count"] = len(sortedPlayers)
	metadata["pair_count"] = len(pairs)
	
	return pairs, metadata, nil
}

// sortPlayersBySeeding sorts players based on seeding method
func (ps *PairingService) sortPlayersBySeeding(players []models.TournamentSubscription, 
	seedings map[string]models.PlayerSeeding, method string) []models.TournamentSubscription {
	
	switch method {
	case "RANK_BASED":
		// Sort by seed number (lower is better)
		sort.Slice(players, func(i, j int) bool {
			seedI, hasI := seedings[players[i].ExternalUserID]
			seedJ, hasJ := seedings[players[j].ExternalUserID]
			
			if !hasI && !hasJ {
				return players[i].JoinedAt.Before(players[j].JoinedAt)
			}
			if !hasI {
				return false // Unseeded players go last
			}
			if !hasJ {
				return true
			}
			return seedI.SeedNumber < seedJ.SeedNumber
		})
		
	case "SKILL_BASED":
		// Sort by skill rating (higher is better)
		sort.Slice(players, func(i, j int) bool {
			seedI, hasI := seedings[players[i].ExternalUserID]
			seedJ, hasJ := seedings[players[j].ExternalUserID]
			
			if !hasI && !hasJ {
				return players[i].JoinedAt.Before(players[j].JoinedAt)
			}
			if !hasI {
				return false
			}
			if !hasJ {
				return true
			}
			return seedI.SkillRating > seedJ.SkillRating
		})
		
	case "RANDOM":
		// Shuffle players randomly
		rand.Seed(time.Now().UnixNano())
		rand.Shuffle(len(players), func(i, j int) {
			players[i], players[j] = players[j], players[i]
		})
	}
	// For "CUSTOM", return as-is for manual adjustment
	
	return players
}

// generateEliminationPairs generates pairs for elimination brackets
func (ps *PairingService) generateEliminationPairs(players []models.TournamentSubscription, matchType string) ([]Pair, map[string]interface{}) {
	var pairs []Pair
	n := len(players)
	
	// For odd number of players, create a bye
	hasBye := n%2 != 0
	
	// Seed 1 vs Seed n, Seed 2 vs Seed n-1, etc.
	for i := 0; i < n/2; i++ {
		p1 := players[i]
		p2 := players[n-i-1]
		
		pairs = append(pairs, Pair{
			Player1ID:   p1.ExternalUserID,
			Player1Name: p1.UserName,
			Player2ID:   p2.ExternalUserID,
			Player2Name: p2.UserName,
			MatchNumber: i + 1,
			RoundNumber: 1,
		})
	}
	
	metadata := map[string]interface{}{
		"bracket_type":    matchType,
		"total_players":   n,
		"has_bye":         hasBye,
		"bye_player":      nil,
	}
	
	if hasBye && n > 0 {
		byePlayer := players[n/2]
		metadata["bye_player"] = map[string]interface{}{
			"id":   byePlayer.ExternalUserID,
			"name": byePlayer.UserName,
		}
	}
	
	return pairs, metadata
}

// generateRoundRobinPairs generates all possible pairs for round robin
func (ps *PairingService) generateRoundRobinPairs(players []models.TournamentSubscription) ([]Pair, map[string]interface{}) {
	var pairs []Pair
	n := len(players)
	matchNum := 1
	
	// Generate all unique pairs
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			pairs = append(pairs, Pair{
				Player1ID:   players[i].ExternalUserID,
				Player1Name: players[i].UserName,
				Player2ID:   players[j].ExternalUserID,
				Player2Name: players[j].UserName,
				MatchNumber: matchNum,
			})
			matchNum++
		}
	}
	
	metadata := map[string]interface{}{
		"format":        "round_robin",
		"total_players": n,
		"total_matches": len(pairs),
		"rounds_needed": n - 1 + n%2,
	}
	
	return pairs, metadata
}

// generateLeaderboardPairs creates initial pairings for leaderboard (all play simultaneously)
func (ps *PairingService) generateLeaderboardPairs(players []models.TournamentSubscription) ([]Pair, map[string]interface{}) {
	var pairs []Pair
	n := len(players)
	
	// For leaderboard, create individual slots rather than head-to-head pairs
	// Or create round robin style if needed
	if n <= 8 {
		// For small groups, create round robin
		return ps.generateRoundRobinPairs(players)
	}
	
	// For large groups, create groups
	metadata := map[string]interface{}{
		"format":         "leaderboard",
		"total_players":  n,
		"simultaneous":   true,
		"scoring_type":   "individual",
	}
	
	return pairs, metadata
}

// generateSwissPairs generates Swiss system pairings
func (ps *PairingService) generateSwissPairs(players []models.TournamentSubscription, round int) ([]Pair, map[string]interface{}) {
	var pairs []Pair
	n := len(players)
	
	// Sort by current score (for later rounds) or seeding (for first round)
	// This is simplified - in reality you'd track scores from previous rounds
	
	// Pair top half with bottom half
	for i := 0; i < n/2; i++ {
		p1 := players[i]
		p2 := players[n/2+i]
		
		pairs = append(pairs, Pair{
			Player1ID:   p1.ExternalUserID,
			Player1Name: p1.UserName,
			Player2ID:   p2.ExternalUserID,
			Player2Name: p2.UserName,
			MatchNumber: i + 1,
			RoundNumber: round,
		})
	}
	
	metadata := map[string]interface{}{
		"format":        "swiss",
		"round":         round,
		"total_players": n,
		"pairing_rule":  "top_vs_bottom",
	}
	
	return pairs, metadata
}

// generateSimplePairs creates simple sequential pairings
func (ps *PairingService) generateSimplePairs(players []models.TournamentSubscription) ([]Pair, map[string]interface{}) {
	var pairs []Pair
	n := len(players)
	
	for i := 0; i < n; i += 2 {
		if i+1 < n {
			p1 := players[i]
			p2 := players[i+1]
			
			pairs = append(pairs, Pair{
				Player1ID:   p1.ExternalUserID,
				Player1Name: p1.UserName,
				Player2ID:   p2.ExternalUserID,
				Player2Name: p2.UserName,
				MatchNumber: i/2 + 1,
			})
		}
	}
	
	metadata := map[string]interface{}{
		"format":        "simple",
		"total_players": n,
		"has_bye":       n%2 != 0,
	}
	
	return pairs, metadata
}

// applyManualOverrides applies manual adjustments to auto-generated pairs
func (ps *PairingService) applyManualOverrides(autoPairs []Pair, manualPairs []Pair) []Pair {
	// Create a map of match numbers to manual pairs
	manualMap := make(map[int]Pair)
	for _, mp := range manualPairs {
		manualMap[mp.MatchNumber] = mp
	}
	
	// Apply manual overrides
	for i, pair := range autoPairs {
		if manualPair, exists := manualMap[pair.MatchNumber]; exists {
			autoPairs[i] = manualPair
			delete(manualMap, pair.MatchNumber)
		}
	}
	
	// Add any new manual pairs
	for _, manualPair := range manualMap {
		autoPairs = append(autoPairs, manualPair)
	}
	
	return autoPairs
}

// UpdatePairings allows editing of proposed pairings
func (ps *PairingService) UpdatePairings(c *fiber.Ctx) error {
	pairingID := c.Params("pairing_id")
	
	type UpdateRequest struct {
		Pairs []Pair `json:"pairs" validate:"required"`
		Notes string `json:"notes"`
	}
	
	var req UpdateRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}
	
	// Fetch pairing
	var pairing models.MatchPairing
	if err := ps.DB.First(&pairing, "id = ?", pairingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "pairing not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "database error"})
	}
	
	// Check if pairing can be edited
	if pairing.Status != "proposed" && pairing.Status != "pending" {
		return c.Status(400).JSON(fiber.Map{"error": "pairing cannot be edited in current status"})
	}
	
	// Update pairs
	pairsJSON, _ := json.Marshal(req.Pairs)
	
	// Create a new version of the pairing
	newPairing := models.MatchPairing{
		ID:           uuid.NewString(),
		MatchID:      pairing.MatchID,
		TournamentID: pairing.TournamentID,
		BatchID:      pairing.BatchID,
		PairingType:  pairing.PairingType,
		SeedingMethod: pairing.SeedingMethod,
		AlgorithmUsed: pairing.AlgorithmUsed,
		PairsJSON:    string(pairsJSON),
		MetadataJSON: pairing.MetadataJSON,
		Status:       "proposed",
		ProposedBy:   c.Locals("user_id").(string),
		ProposedAt:   time.Now(),
		Version:      pairing.Version + 1,
	}
	
	if err := ps.DB.Create(&newPairing).Error; err != nil {
		log.Printf("DB Error updating pairing: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to update pairing"})
	}
	
	// Update match reference
	ps.DB.Model(&models.TournamentMatch{}).
		Where("id = ?", pairing.MatchID).
		Update("current_pairing_id", newPairing.ID)
	
	return c.JSON(PairingResponse{
		MatchID:    newPairing.MatchID,
		PairingID:  newPairing.ID,
		Status:     newPairing.Status,
		Pairs:      req.Pairs,
		TotalPairs: len(req.Pairs),
		ProposedAt: newPairing.ProposedAt,
		CanEdit:    true,
		CanApprove: true,
		CanPublish: false,
	})
}

// ApprovePairings approves the proposed pairings
func (ps *PairingService) ApprovePairings(c *fiber.Ctx) error {
	pairingID := c.Params("pairing_id")
	userID := c.Locals("user_id").(string)
	
	type ApproveRequest struct {
		Finalize bool `json:"finalize"`
	}
	
	var req ApproveRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}
	
	// Fetch pairing
	var pairing models.MatchPairing
	if err := ps.DB.First(&pairing, "id = ?", pairingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "pairing not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "database error"})
	}
	
	// Check if pairing can be approved
	if pairing.Status != "proposed" {
		return c.Status(400).JSON(fiber.Map{"error": "pairing cannot be approved in current status"})
	}
	
	// Update pairing status
	now := time.Now()
	updates := map[string]interface{}{
		"status":      "approved",
		"approved_by": userID,
		"approved_at": &now,
	}
	
	if err := ps.DB.Model(&pairing).Updates(updates).Error; err != nil {
		log.Printf("DB Error approving pairing: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to approve pairing"})
	}
	
	// If finalize is true, also publish
	if req.Finalize {
		return ps.publishPairingsInternal(pairing.ID, userID)
	}
	
	return c.JSON(fiber.Map{
		"message":   "pairings approved successfully",
		"pairing_id": pairing.ID,
		"status":    "approved",
		"approved_at": now,
	})
}

// PublishPairings publishes approved pairings
func (ps *PairingService) PublishPairings(c *fiber.Ctx) error {
	pairingID := c.Params("pairing_id")
	userID := c.Locals("user_id").(string)
	
	return ps.publishPairingsInternal(pairingID, userID)
}

func (ps *PairingService) publishPairingsInternal(pairingID string, userID string) error {
	// Fetch pairing
	var pairing models.MatchPairing
	if err := ps.DB.First(&pairing, "id = ?", pairingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fiber.NewError(404, "pairing not found")
		}
		return fiber.NewError(500, "database error")
	}
	
	// Check if pairing can be published
	if pairing.Status != "approved" {
		return fiber.NewError(400, "pairing cannot be published in current status")
	}
	
	// Parse pairs
	var pairs []Pair
	if err := json.Unmarshal([]byte(pairing.PairsJSON), &pairs); err != nil {
		return fiber.NewError(500, "failed to parse pairing data")
	}
	
	// Update match with actual player assignments
	for _, pair := range pairs {
		// Update the match record with player assignments
		// This would depend on your match structure
		// For 1v1 matches, update player1 and player2 fields
		// For team matches, update differently
		// Note: Since we removed player fields from TournamentMatch,
		// this logic needs to be updated based on your new design
		log.Printf("Would assign players to match: %s vs %s", pair.Player1ID, pair.Player2ID)
	}
	
	// Update pairing status
	now := time.Now()
	updates := map[string]interface{}{
		"status":       "published",
		"published_by": userID,
		"published_at": &now,
	}
	
	if err := ps.DB.Model(&pairing).Updates(updates).Error; err != nil {
		log.Printf("DB Error publishing pairing: %v", err)
		return fiber.NewError(500, "failed to publish pairing")
	}
	
	return nil
}

func (s *PairingService) GetPairingStatus(c *fiber.Ctx) error {
    matchId := c.Params("match_id")
    
    // Fetch the latest pairing for this match
    var pairing models.MatchPairing
    // Change from: ORDER BY created_at DESC
    // To: ORDER BY proposed_at DESC
    if err := s.DB.Where("match_id = ?", matchId).
        Order("proposed_at DESC").
        First(&pairing).Error; err != nil {
        
        if errors.Is(err, gorm.ErrRecordNotFound) {
            return c.JSON(fiber.Map{
                "status":       "no_pairing",
                "pairing_id":   nil,
                "match_id":     matchId,
                "has_pairing":  false,
                "message":      "No pairing has been generated yet",
            })
        }
        
        log.Printf("ERROR fetching pairing status for match %s: %v", matchId, err)
        return c.Status(500).JSON(fiber.Map{"error": "database error"})
    }
    
    // Count number of players in the pairing
    var pairCount int
    if pairing.PairsJSON != "" {
        var pairs []map[string]interface{}
        if err := json.Unmarshal([]byte(pairing.PairsJSON), &pairs); err == nil {
            pairCount = len(pairs)
        }
    }
    
    return c.JSON(fiber.Map{
        "status":        pairing.Status,
        "pairing_id":    pairing.ID,
        "match_id":      pairing.MatchID,
        "tournament_id": pairing.TournamentID,
        "batch_id":      pairing.BatchID,
        "has_pairing":   true,
        "version":       pairing.Version,
        "pair_count":    pairCount,
        "proposed_by":   pairing.ProposedBy,
        "proposed_at":   pairing.ProposedAt,
        "approved_by":   pairing.ApprovedBy,
        "approved_at":   pairing.ApprovedAt,
        "published_by":  pairing.PublishedBy,
        "published_at":  pairing.PublishedAt,
        "rejected_by":   pairing.RejectedBy,
        "rejected_at":   pairing.RejectedAt,
        "rejection_reason": pairing.RejectionReason,
    })
}
// RejectPairings rejects the proposed pairings
func (ps *PairingService) RejectPairings(c *fiber.Ctx) error {
	pairingID := c.Params("pairing_id")
	userID := c.Locals("user_id").(string)
	
	type RejectRequest struct {
		Reason string `json:"reason" validate:"required"`
	}
	
	var req RejectRequest
	if err := c.BodyParser(&req); err != nil {
		return c.Status(400).JSON(fiber.Map{"error": "invalid JSON", "details": err.Error()})
	}
	
	// Fetch pairing
	var pairing models.MatchPairing
	if err := ps.DB.First(&pairing, "id = ?", pairingID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return c.Status(404).JSON(fiber.Map{"error": "pairing not found"})
		}
		return c.Status(500).JSON(fiber.Map{"error": "database error"})
	}
	
	// Check if pairing can be rejected
	if pairing.Status != "proposed" && pairing.Status != "pending" {
		return c.Status(400).JSON(fiber.Map{"error": "pairing cannot be rejected in current status"})
	}
	
	// Update pairing status
	now := time.Now()
	updates := map[string]interface{}{
		"status":          "rejected",
		"rejected_by":     userID,
		"rejected_at":     &now,
		"rejection_reason": req.Reason,
	}
	
	if err := ps.DB.Model(&pairing).Updates(updates).Error; err != nil {
		log.Printf("DB Error rejecting pairing: %v", err)
		return c.Status(500).JSON(fiber.Map{"error": "failed to reject pairing"})
	}
	
	return c.JSON(fiber.Map{
		"message":   "pairings rejected",
		"pairing_id": pairing.ID,
		"status":    "rejected",
		"rejected_at": now,
		"reason":    req.Reason,
	})
}

func (s *PairingService) GetPairingHistory(c *fiber.Ctx) error {
    matchId := c.Params("match_id")
    
    var pairings []models.MatchPairing
    // Change from: ORDER BY created_at DESC
    // To: ORDER BY proposed_at DESC
    if err := s.DB.Where("match_id = ?", matchId).
        Order("proposed_at DESC").
        Find(&pairings).Error; err != nil {
        
        log.Printf("ERROR fetching pairing history for match %s: %v", matchId, err)
        return c.Status(500).JSON(fiber.Map{"error": "database error"})
    }
    
    // Format the response
    var history []map[string]interface{}
    for _, p := range pairings {
        history = append(history, map[string]interface{}{
            "id":          p.ID,
            "status":      p.Status,
            "version":     p.Version,
            "proposed_by": p.ProposedBy,
            "proposed_at": p.ProposedAt,
            "approved_by": p.ApprovedBy,
            "approved_at": p.ApprovedAt,
            "published_by": p.PublishedBy,
            "published_at": p.PublishedAt,
            "rejected_by": p.RejectedBy,
            "rejected_at": p.RejectedAt,
            "rejection_reason": p.RejectionReason,
        })
    }
    
    return c.JSON(fiber.Map{
        "match_id": matchId,
        "history":  history,
        "count":    len(history),
    })
}