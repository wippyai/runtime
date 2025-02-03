local bapp = require("bapp")  -- Change to correct bapp import

function App()
    local app = bapp.new()

    -- Define colors
    app.colors = {
        highlight = "#7D56F4",
        fg = "#CDD6F4",
        muted = "#6C7086",
        bg = "#1E1E2E"
    }

    -- Create styles
    app.styles = {
        container = btea.new_style()
            :padding(1)
            :background(app.colors.bg)
            :border(btea.borders.ROUNDED)
            :border_foreground(app.colors.highlight),

        title = btea.new_style()
            :foreground(app.colors.fg)
            :bold(),

        help = btea.new_style()
            :foreground(app.colors.muted)
            :italic()
    }

    -- Sample data
    app.items = {
        "First item in the list",
        "Second item with some extra text",
        "Third item that's quite descriptive",
        "Fourth item showing pagination",
        "Fifth item in our demo",
        "Sixth item with details",
        "Seventh item demonstrating scrolling",
        "Eighth item in the sequence",
        "Ninth item with information",
        "Tenth item showing more",
        "Eleventh item for testing",
        "Twelfth item in our list",
        "Thirteenth item here",
        "Fourteenth item example",
        "Fifteenth item displayed",
    }

    -- Create paginator
    app.paginator = btea.new_paginator({
        type = btea.paginator_types.DOTS,
        per_page = 5,
        active_dot = "●",
        inactive_dot = "○"
    })
    app.paginator:set_total_pages(#app.items)

    -- Setup key bindings with help text
    app.keys = bapp.create_keys({
        prev = {
            keys = {"left", "h", "p"},
            help = {key = "←/h", desc = "previous page"}
        },
        next = {
            keys = {"right", "l", "n"},
            help = {key = "→/l", desc = "next page"}
        },
        toggle_style = {
            keys = {"t"},
            help = {key = "t", desc = "toggle style"}
        }
    })

    -- Update function
    local function update(self, msg)
        if msg.key then
            if self.keys.quit:matches(msg) then
                return true
            elseif self.keys.prev:matches(msg) then
                self.paginator:prev_page()
            elseif self.keys.next:matches(msg) then
                self.paginator:next_page()
            elseif self.keys.toggle_style:matches(msg) then
                -- Toggle between DOTS and ARABIC style
                if self.paginator:get_type() == btea.paginator_types.DOTS then
                    self.paginator:set_type(btea.paginator_types.ARABIC)
                else
                    self.paginator:set_type(btea.paginator_types.DOTS)
                end
            end
        end
        return false
    end

    -- View function
    local function view(self)
        -- Get current page items
        local start, end_ = self.paginator:get_slice_bounds(#self.items)
        local current_items = {}
        for i = start + 1, end_ do
            table.insert(current_items, string.format("%d. %s", i, self.items[i]))
        end

        -- Build the view
        local lines = {
            self.styles.title:render("Paginator Demo"),
            "",
            table.concat(current_items, "\n"),
            "",
            self.paginator:view(),
            "",
            self.styles.help:render("←/h previous | →/l next | t toggle style | q quit")
        }

        return self.styles.container:render(table.concat(lines, "\n"))
    end

    -- Run the app
    app:run(update, view)
end

return App