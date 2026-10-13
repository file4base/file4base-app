package schema

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/file4base/file4base-app/server/internal/dbal"
	"github.com/google/uuid"
)

// ErrMenuSetNotFound is returned when no menu set has the given id.
var ErrMenuSetNotFound = errors.New("menu set not found")

// ErrMenuSetExists is returned when another set already has that name.
var ErrMenuSetExists = errors.New("a menu set with that name already exists")

// ErrInvalidMenuSet is returned for a set that cannot be stored as given.
var ErrInvalidMenuSet = errors.New("invalid menu set")

// Bounds on a menu set, so a stored set stays something a designer wrote.
const (
	maxMenuSetName   = 128
	maxCustomMenus   = 12
	maxMenuItems     = 40
	maxMenuItemTitle = 64
)

// CustomisableMenus are the menus a solution may change. **File is not one of
// them as a whole**: it may only be extended, so signing out, opening a
// solution and creating one stay reachable whatever a menu set says. Help is
// not customisable at all — it is how a user finds their way back.
var CustomisableMenus = []string{
	"File", "Edit", "View", "Insert", "Format", "Records", "Requests", "Scripts", "Tools", "Window",
}

// ExtendOnlyMenus may have items added but never replaced.
var ExtendOnlyMenus = []string{"File"}

// Menu set modes.
const (
	// MenuModeExtend adds the items after the standard ones.
	MenuModeExtend = "extend"
	// MenuModeReplace puts the items in place of the standard ones.
	MenuModeReplace = "replace"
)

// MenuItem is one entry of a custom menu. An item with an empty title and no
// action is a separator.
//
// What an item does is a **button action**: the same shape a layout button
// stores, run by the same runner, so a menu item cannot do anything a button
// could not, and nothing new had to be made runnable.
type MenuItem struct {
	Title     string                 `json:"title" msgpack:"title"`
	Shortcut  string                 `json:"shortcut,omitempty" msgpack:"shortcut,omitempty"`
	Action    map[string]interface{} `json:"action,omitempty" msgpack:"action,omitempty"`
	Separator bool                   `json:"separator,omitempty" msgpack:"separator,omitempty"`
}

// CustomMenu is one menu of the bar, as a solution redefines it.
type CustomMenu struct {
	Menu  string     `json:"menu" msgpack:"menu"`
	Mode  string     `json:"mode" msgpack:"mode"`
	Items []MenuItem `json:"items" msgpack:"items"`
}

// MenuSet is a named set of menus a solution installs (#52).
//
// A menu set is a convenience, not a security boundary: what an account may
// actually do is decided by its role, its table and layout access and the
// extended privileges, and the server enforces those whatever the menu bar
// shows.
type MenuSet struct {
	ID        string       `json:"id"`
	Name      string       `json:"name"`
	Menus     []CustomMenu `json:"menus"`
	IsDefault bool         `json:"is_default"`
	CreatedAt time.Time    `json:"created_at"`
	UpdatedAt time.Time    `json:"updated_at"`
}

// MenuSetInput is what a caller supplies to create or update a set.
type MenuSetInput struct {
	Name      string       `json:"name"`
	Menus     []CustomMenu `json:"menus"`
	IsDefault bool         `json:"is_default"`
}

func (s *Service) menuSetPlaceholder(n int) string {
	if s.driver.Dialect().Engine() == dbal.EngineMariaDB {
		return "?"
	}
	return fmt.Sprintf("$%d", n)
}

func knownMenu(name string) bool {
	for _, menu := range CustomisableMenus {
		if strings.EqualFold(menu, name) {
			return true
		}
	}
	return false
}

func extendOnly(name string) bool {
	for _, menu := range ExtendOnlyMenus {
		if strings.EqualFold(menu, name) {
			return true
		}
	}
	return false
}

// checkMenuSet validates a set before it is written.
func checkMenuSet(in MenuSetInput) (MenuSetInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return in, fmt.Errorf("%w: a menu set needs a name", ErrInvalidMenuSet)
	}
	if len([]rune(in.Name)) > maxMenuSetName {
		return in, fmt.Errorf("%w: the name is at most %d characters", ErrInvalidMenuSet, maxMenuSetName)
	}
	if len(in.Menus) == 0 {
		return in, fmt.Errorf("%w: a menu set changes at least one menu", ErrInvalidMenuSet)
	}
	if len(in.Menus) > maxCustomMenus {
		return in, fmt.Errorf("%w: a menu set changes at most %d menus", ErrInvalidMenuSet, maxCustomMenus)
	}

	seen := map[string]bool{}
	seenShortcuts := map[string]string{}
	cleaned := make([]CustomMenu, 0, len(in.Menus))
	for _, menu := range in.Menus {
		name := strings.TrimSpace(menu.Menu)
		if !knownMenu(name) {
			return in, fmt.Errorf("%w: %q is not a menu a solution can change (%s)",
				ErrInvalidMenuSet, name, strings.Join(CustomisableMenus, ", "))
		}
		if seen[strings.ToLower(name)] {
			return in, fmt.Errorf("%w: the %s menu is given twice", ErrInvalidMenuSet, name)
		}
		seen[strings.ToLower(name)] = true

		mode := strings.ToLower(strings.TrimSpace(menu.Mode))
		if mode == "" {
			mode = MenuModeExtend
		}
		if mode != MenuModeExtend && mode != MenuModeReplace {
			return in, fmt.Errorf("%w: a menu is %q or %q, not %q",
				ErrInvalidMenuSet, MenuModeExtend, MenuModeReplace, menu.Mode)
		}
		if mode == MenuModeReplace && extendOnly(name) {
			return in, fmt.Errorf(
				"%w: the %s menu can only be extended, so that signing out and opening a solution stay reachable",
				ErrInvalidMenuSet, name)
		}

		if len(menu.Items) == 0 {
			return in, fmt.Errorf("%w: the %s menu has no items", ErrInvalidMenuSet, name)
		}
		if len(menu.Items) > maxMenuItems {
			return in, fmt.Errorf("%w: the %s menu has more than %d items", ErrInvalidMenuSet, name, maxMenuItems)
		}

		items := make([]MenuItem, 0, len(menu.Items))
		real := 0
		for i, item := range menu.Items {
			item.Title = strings.TrimSpace(item.Title)
			item.Shortcut = strings.TrimSpace(item.Shortcut)

			if item.Separator || (item.Title == "" && len(item.Action) == 0) {
				items = append(items, MenuItem{Separator: true})
				continue
			}
			if item.Title == "" {
				return in, fmt.Errorf("%w: item %d of the %s menu has no title", ErrInvalidMenuSet, i+1, name)
			}
			if len([]rune(item.Title)) > maxMenuItemTitle {
				return in, fmt.Errorf("%w: the title %q is longer than %d characters",
					ErrInvalidMenuSet, item.Title, maxMenuItemTitle)
			}
			if len(item.Action) == 0 {
				return in, fmt.Errorf("%w: %q does nothing; give it a script or a step",
					ErrInvalidMenuSet, item.Title)
			}
			if err := checkMenuAction(item.Title, item.Action); err != nil {
				return in, err
			}
			if item.Shortcut != "" {
				norm, err := normalizeShortcut(item.Title, item.Shortcut)
				if err != nil {
					return in, err
				}
				if prev, dup := seenShortcuts[norm]; dup {
					return in, fmt.Errorf("%w: %q uses the same shortcut (%s) as %q",
						ErrInvalidMenuSet, item.Title, item.Shortcut, prev)
				}
				seenShortcuts[norm] = item.Title
			}
			items = append(items, item)
			real++
		}
		if real == 0 {
			return in, fmt.Errorf("%w: the %s menu is nothing but separators", ErrInvalidMenuSet, name)
		}
		cleaned = append(cleaned, CustomMenu{Menu: name, Mode: mode, Items: items})
	}
	in.Menus = cleaned
	return in, nil
}

// reservedModeShortcuts are the primary mode and application shortcuts that a
// custom menu set may never shadow (#52).
var reservedModeShortcuts = map[string]string{
	"mod+b": "Browse Mode (Cmd/Ctrl+B)",
	"mod+f": "Find Mode (Cmd/Ctrl+F)",
	"mod+l": "Layout Mode (Cmd/Ctrl+L)",
	"mod+u": "Preview Mode (Cmd/Ctrl+U)",
	"mod+q": "Quit (Cmd/Ctrl+Q)",
}

func normalizeShortcut(title, raw string) (string, error) {
	parts := strings.Split(raw, "+")
	var hasMod, hasShift, hasAlt bool
	var key string
	for _, p := range parts {
		tok := strings.ToLower(strings.TrimSpace(p))
		if tok == "" {
			return "", fmt.Errorf("%w: %q has a malformed shortcut %q", ErrInvalidMenuSet, title, raw)
		}
		switch tok {
		case "cmd", "command", "meta", "ctrl", "control":
			hasMod = true
		case "shift":
			hasShift = true
		case "alt", "option", "opt":
			hasAlt = true
		default:
			if key != "" {
				return "", fmt.Errorf("%w: %q shortcut %q names more than one key", ErrInvalidMenuSet, title, raw)
			}
			key = tok
		}
	}
	if key == "" {
		return "", fmt.Errorf("%w: %q shortcut %q has no key", ErrInvalidMenuSet, title, raw)
	}
	mods := make([]string, 0, 4)
	if hasMod {
		mods = append(mods, "mod")
	}
	if hasAlt {
		mods = append(mods, "alt")
	}
	if hasShift {
		mods = append(mods, "shift")
	}
	mods = append(mods, key)
	norm := strings.Join(mods, "+")
	if reserved, blocked := reservedModeShortcuts[norm]; blocked {
		return "", fmt.Errorf("%w: %q cannot use %q because it is reserved for %s",
			ErrInvalidMenuSet, title, raw, reserved)
	}
	return norm, nil
}

// checkMenuAction makes sure an item does something the runner can run: a
// script, or one of the steps a layout button takes.
func checkMenuAction(title string, action map[string]interface{}) error {
	kind, _ := action["type"].(string)
	switch strings.TrimSpace(kind) {
	case "perform_script":
		id, _ := action["script_id"].(string)
		name, _ := action["script_name"].(string)
		if strings.TrimSpace(id) == "" && strings.TrimSpace(name) == "" {
			return fmt.Errorf("%w: %q performs a script but names none", ErrInvalidMenuSet, title)
		}
	case "single_step":
		step, _ := action["step_type"].(string)
		if strings.TrimSpace(step) == "" {
			return fmt.Errorf("%w: %q runs a step but names none", ErrInvalidMenuSet, title)
		}
	default:
		return fmt.Errorf("%w: %q must perform a script or run a single step", ErrInvalidMenuSet, title)
	}
	return nil
}

// CreateMenuSet stores a new set.
func (s *Service) CreateMenuSet(ctx context.Context, in MenuSetInput) (*MenuSet, error) {
	in, err := checkMenuSet(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.menuSetNameTaken(ctx, in.Name, ""); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrMenuSetExists, in.Name)
	}

	encoded, err := json.Marshal(in.Menus)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	set := &MenuSet{
		ID: uuid.NewString(), Name: in.Name, Menus: in.Menus,
		IsDefault: in.IsDefault, CreatedAt: now, UpdatedAt: now,
	}

	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if in.IsDefault {
		if err := s.clearDefaultMenuSet(ctx, tx, ""); err != nil {
			return nil, err
		}
	}
	q := fmt.Sprintf(`INSERT INTO sys_menu_sets (id, name, definition, is_default, created_at, updated_at)
	                  VALUES (%s, %s, %s, %s, %s, %s)`,
		s.menuSetPlaceholder(1), s.menuSetPlaceholder(2), s.menuSetPlaceholder(3),
		s.menuSetPlaceholder(4), s.menuSetPlaceholder(5), s.menuSetPlaceholder(6))
	if _, err := tx.ExecContext(ctx, q, set.ID, set.Name, string(encoded), set.IsDefault,
		set.CreatedAt, set.UpdatedAt); err != nil {
		return nil, fmt.Errorf("failed creating menu set %q: %w", set.Name, err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return set, nil
}

// UpdateMenuSet replaces a set's name and menus.
func (s *Service) UpdateMenuSet(ctx context.Context, id string, in MenuSetInput) (*MenuSet, error) {
	in, err := checkMenuSet(in)
	if err != nil {
		return nil, err
	}
	if taken, err := s.menuSetNameTaken(ctx, in.Name, id); err != nil {
		return nil, err
	} else if taken {
		return nil, fmt.Errorf("%w: %s", ErrMenuSetExists, in.Name)
	}

	encoded, err := json.Marshal(in.Menus)
	if err != nil {
		return nil, err
	}

	tx, err := s.driver.DB().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if in.IsDefault {
		if err := s.clearDefaultMenuSet(ctx, tx, id); err != nil {
			return nil, err
		}
	}
	q := fmt.Sprintf(`UPDATE sys_menu_sets SET name = %s, definition = %s, is_default = %s, updated_at = %s
	                  WHERE id = %s`,
		s.menuSetPlaceholder(1), s.menuSetPlaceholder(2), s.menuSetPlaceholder(3),
		s.menuSetPlaceholder(4), s.menuSetPlaceholder(5))
	res, err := tx.ExecContext(ctx, q, in.Name, string(encoded), in.IsDefault, time.Now().UTC(), id)
	if err != nil {
		return nil, fmt.Errorf("failed updating menu set %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return nil, fmt.Errorf("%w: %s", ErrMenuSetNotFound, id)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetMenuSet(ctx, id)
}

// clearDefaultMenuSet leaves at most one set marked as the solution's.
func (s *Service) clearDefaultMenuSet(ctx context.Context, tx *sql.Tx, exceptID string) error {
	q := fmt.Sprintf(`UPDATE sys_menu_sets SET is_default = %s WHERE id <> %s`,
		s.menuSetPlaceholder(1), s.menuSetPlaceholder(2))
	_, err := tx.ExecContext(ctx, q, false, exceptID)
	return err
}

// DeleteMenuSet removes a set. A layout that named it falls back to the
// standard menus, so nothing breaks.
func (s *Service) DeleteMenuSet(ctx context.Context, id string) error {
	q := fmt.Sprintf(`DELETE FROM sys_menu_sets WHERE id = %s`, s.menuSetPlaceholder(1))
	res, err := s.driver.DB().ExecContext(ctx, q, id)
	if err != nil {
		return fmt.Errorf("failed deleting menu set %s: %w", id, err)
	}
	if affected, _ := res.RowsAffected(); affected == 0 {
		return fmt.Errorf("%w: %s", ErrMenuSetNotFound, id)
	}
	return nil
}

func scanMenuSet(row rowScanner) (*MenuSet, error) {
	var set MenuSet
	var encoded string
	if err := row.Scan(&set.ID, &set.Name, &encoded, &set.IsDefault, &set.CreatedAt, &set.UpdatedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(encoded), &set.Menus); err != nil {
		return nil, fmt.Errorf("the menu set %q cannot be read: %w", set.Name, err)
	}
	return &set, nil
}

// GetMenuSet reads one set.
func (s *Service) GetMenuSet(ctx context.Context, id string) (*MenuSet, error) {
	q := fmt.Sprintf(`SELECT id, name, definition, is_default, created_at, updated_at
	                  FROM sys_menu_sets WHERE id = %s`, s.menuSetPlaceholder(1))
	set, err := scanMenuSet(s.driver.DB().QueryRowContext(ctx, q, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, fmt.Errorf("%w: %s", ErrMenuSetNotFound, id)
	}
	if err != nil {
		return nil, err
	}
	return set, nil
}

// ListMenuSets reads every set, by name.
func (s *Service) ListMenuSets(ctx context.Context) ([]MenuSet, error) {
	rows, err := s.driver.DB().QueryContext(ctx,
		`SELECT id, name, definition, is_default, created_at, updated_at FROM sys_menu_sets ORDER BY name ASC`)
	if err != nil {
		return nil, fmt.Errorf("failed reading the menu sets: %w", err)
	}
	defer rows.Close()

	sets := []MenuSet{}
	for rows.Next() {
		set, err := scanMenuSet(rows)
		if err != nil {
			return nil, err
		}
		sets = append(sets, *set)
	}
	return sets, rows.Err()
}

func (s *Service) menuSetNameTaken(ctx context.Context, name, exceptID string) (bool, error) {
	q := fmt.Sprintf(`SELECT COUNT(*) FROM sys_menu_sets WHERE LOWER(name) = LOWER(%s) AND id <> %s`,
		s.menuSetPlaceholder(1), s.menuSetPlaceholder(2))
	var count int
	if err := s.driver.DB().QueryRowContext(ctx, q, name, exceptID).Scan(&count); err != nil {
		return false, fmt.Errorf("failed checking the name %q: %w", name, err)
	}
	return count > 0, nil
}
