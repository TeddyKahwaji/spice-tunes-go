package pagination

import (
	"github.com/TeddyKahwaji/spice-tunes-go/pkg/views"
	"github.com/bwmarrin/discordgo"
)

// GetPaginationEmbed is a function type used for embedding paginated data into a Discord embed.
// It takes in paginated data, the current page number, and a separator (the number of items per page).
type GetPaginationEmbed[T any] func(data []*T, pageNum int, totalPages int, separator int) *discordgo.MessageEmbed

// PaginationConfig is a generic struct that manages the configuration for paginating a set of data.
// The Data field holds the full dataset, while the Separator specifies how many items should appear per page.
type PaginationConfig[T any] struct {
	Data       []*T // Slice of items to paginate
	Separator  int  // Number of items to display per page
	pageNum    *int // Pointer to the current page number
	totalPages *int // Pointer to the total number of pages
}

// NewPaginatedConfig creates a new PaginationConfig for handling paginated views.
// It initializes the pagination with a dataset and a separator (items per page) and sets the initial page number to 1.
func NewPaginatedConfig[T any](data []*T, separator int) *PaginationConfig[T] {
	startingPageNum := 1
	startingTotalPages := 0

	return &PaginationConfig[T]{
		Data:       data,
		Separator:  separator,
		pageNum:    &startingPageNum,
		totalPages: &startingTotalPages,
	}
}

// PaginationListButtonsConfig defines the state of the pagination buttons.
// Each field determines whether a corresponding button is disabled in the pagination UI.
type PaginationListButtonsConfig struct {
	SkipToLastPageDisabled  bool // Disable the "Skip to Last Page" button
	BackToFirstPageDisabled bool // Disable the "Back to First Page" button
	BackDisabled            bool // Disable the "Back" button
	SkipDisabled            bool // Disable the "Skip" button
}

// GetPaginationListButtons generates pagination buttons based on the provided configuration.
// These buttons allow users to navigate between pages, and they can be disabled depending on the current page state.
func GetPaginationListButtons(config PaginationListButtonsConfig) []discordgo.MessageComponent {
	return []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Disabled: config.BackToFirstPageDisabled,
					CustomID: "FirstPageBtn", // Button to jump to the first page
					Label:    "|<",
					Style:    discordgo.SuccessButton,
				},
				discordgo.Button{
					Disabled: config.BackDisabled,
					CustomID: "BackBtn", // Button to go to the previous page
					Label:    "<",
					Style:    discordgo.PrimaryButton,
				},
				discordgo.Button{
					Disabled: config.SkipDisabled,
					CustomID: "SkipBtn", // Button to go to the next page
					Label:    ">",
					Style:    discordgo.PrimaryButton,
				},
				discordgo.Button{
					Disabled: config.SkipToLastPageDisabled,
					CustomID: "LastPageBtn", // Button to jump to the last page
					Label:    ">|",
					Style:    discordgo.SuccessButton,
				},
			},
		},
	}
}

// GetPages divides the dataset into pages based on the Separator value.
// It returns a 2D slice where each inner slice represents a single page containing a subset of the data.
func (p *PaginationConfig[T]) GetPages() [][]*T {
	pages := [][]*T{}

	// Iterate over the data and split it into pages using the separator.
	for i, elem := range p.Data {
		// Create a new page when the current page reaches the separator limit.
		if i%p.Separator == 0 {
			pages = append(pages, []*T{})
		}

		// Add the item to the current page.
		pages[len(pages)-1] = append(pages[len(pages)-1], elem)
	}

	// Set the total number of pages.
	*p.totalPages = len(pages)

	return pages
}

// GetBaseHandler returns a handler function that processes pagination button clicks.
// It updates the current page number based on the button clicked (First, Back, Next, Last),
// and calls the afterHandler to refresh the embed or perform additional logic.
func (p *PaginationConfig[T]) GetBaseHandler(_ *discordgo.Session, afterHandler views.Handler) views.Handler {
	return func(passedInteraction *discordgo.Interaction) error {
		// Determine which button was clicked based on its custom ID.
		messageCustomID := passedInteraction.MessageComponentData().CustomID

		// Adjust the current page number based on the button that was clicked.
		switch messageCustomID {
		case "FirstPageBtn":
			*p.pageNum = 1 // Jump to the first page
		case "BackBtn":
			*p.pageNum-- // Move to the previous page
		case "SkipBtn":
			*p.pageNum++ // Move to the next page
		case "LastPageBtn":
			*p.pageNum = *p.totalPages // Jump to the last page
		}

		// Call the provided afterHandler to handle additional functionality, such as updating the UI.
		if err := afterHandler(passedInteraction); err != nil {
			return err
		}

		return nil
	}
}

// GetViewConfig generates a ViewConfig that includes paginated embeds and the appropriate pagination buttons.
// It takes a function to generate embeds for each page and constructs a new ViewConfig based on the current page and total pages.
func (p *PaginationConfig[T]) GetViewConfig(paginationEmbedRetriever GetPaginationEmbed[T]) *views.Config {
	// Split the data into pages.
	pages := p.GetPages()
	if len(pages) == 0 {
		// TODO: Add error
		return nil
	}

	// Create embeds for each page using the provided paginationEmbedRetriever function.
	paginationEmbeds := make([]*discordgo.MessageEmbed, 0, *p.totalPages)
	for pageNum, pageData := range pages {
		paginationEmbeds = append(paginationEmbeds, paginationEmbedRetriever(pageData, pageNum+1, len(pages), p.Separator))
	}

	// Determine which pagination buttons should be disabled based on the current page.
	buttonsConfig := PaginationListButtonsConfig{
		SkipToLastPageDisabled:  *p.pageNum == *p.totalPages,
		SkipDisabled:            *p.pageNum == *p.totalPages,
		BackToFirstPageDisabled: *p.pageNum == 1,
		BackDisabled:            *p.pageNum == 1,
	}

	// Generate the pagination buttons using the buttonsConfig.
	paginationButtons := GetPaginationListButtons(buttonsConfig)

	if *p.pageNum-1 >= len(pages) {
		*p.pageNum = 1
	}

	// Return a new ViewConfig with the current page's embed and the corresponding buttons.
	return &views.Config{
		Components: &views.ComponentHandler{
			MessageComponents: paginationButtons,
		},
		Embeds: []*discordgo.MessageEmbed{paginationEmbeds[*p.pageNum-1]},
	}
}
