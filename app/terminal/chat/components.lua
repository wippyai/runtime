local M = {}

-- Styles definition
M.styles = {
    box = btea.style()
        :border(btea.borders.ROUNDED)
        :padding(1, 2)
        :foreground("#89B4FA")
        :background("#1E1E2E"),

    header = btea.style()
        :bold()
        :foreground("#CBA6F7")
        :padding(0, 1)
        :underline(),

    ai = btea.style()
        :foreground("#A6E3A1")
        :italic(),

    user = btea.style()
        :foreground("#F5C2E7"),

    timestamp = btea.style()
        :foreground("#6C7086"),

    error = btea.style()
        :foreground("#F38BA8")
        :italic()
}

M.ChatSession = {
    new = function()
        return {
            messages = {},
            current_response = "",
            is_responding = false,
            current_message = nil,

            add_message = function(self, role, content)
                local msg = { role = role, content = content }
                table.insert(self.messages, msg)
                return msg
            end,

            start_response = function(self)
                self.is_responding = true
                self.current_response = ""
                self.current_message = self:add_message("assistant", "")
            end,

            update_response = function(self, text)
                self.current_response = self.current_response .. text
                if self.current_message then
                    self.current_message.content = self.current_response
                end
            end,

            finish_response = function(self)
                if self.current_message then
                    self.current_message.content = self.current_response
                end
                self.current_response = ""
                self.is_responding = false
                self.current_message = nil
            end,

            clear = function(self)
                self.messages = {}
                self.current_response = ""
                self.is_responding = false
                self.current_message = nil
            end,

            get_history = function(self)
                return self.messages
            end
        }
    end
}

return M
