// Netio DSL - JavaScript Implementation
// A complete port of the Go netio.dsl library for the browser.
var Netio = (function () {
  "use strict";

  // =====================================================================
  // Token Types
  // =====================================================================
  var TOKEN = {
    EOF: 0,
    IDENT: 1,
    STRING: 2,
    INT: 3,
    FLOAT: 4,
    BOOL: 5,
    IP_CIDR: 6,
    COLON: 7,
    EQUALS: 8,
    ARROW: 9,
    DASH: 10,
    NEWLINE: 11,
    INDENT: 12,
    DEDENT: 13,
    INCLUDE: 14,
    EXTENDS: 15,
    COMMENT: 16,
  };

  var TOKEN_NAMES = {};
  TOKEN_NAMES[TOKEN.EOF] = "EOF";
  TOKEN_NAMES[TOKEN.IDENT] = "IDENT";
  TOKEN_NAMES[TOKEN.STRING] = "STRING";
  TOKEN_NAMES[TOKEN.INT] = "INT";
  TOKEN_NAMES[TOKEN.FLOAT] = "FLOAT";
  TOKEN_NAMES[TOKEN.BOOL] = "BOOL";
  TOKEN_NAMES[TOKEN.IP_CIDR] = "IP_CIDR";
  TOKEN_NAMES[TOKEN.COLON] = "COLON";
  TOKEN_NAMES[TOKEN.EQUALS] = "EQUALS";
  TOKEN_NAMES[TOKEN.ARROW] = "ARROW";
  TOKEN_NAMES[TOKEN.DASH] = "DASH";
  TOKEN_NAMES[TOKEN.NEWLINE] = "NEWLINE";
  TOKEN_NAMES[TOKEN.INDENT] = "INDENT";
  TOKEN_NAMES[TOKEN.DEDENT] = "DEDENT";
  TOKEN_NAMES[TOKEN.INCLUDE] = "INCLUDE";
  TOKEN_NAMES[TOKEN.EXTENDS] = "EXTENDS";
  TOKEN_NAMES[TOKEN.COMMENT] = "COMMENT";

  function tokenName(type) {
    return TOKEN_NAMES[type] || "TOKEN(" + type + ")";
  }

  function Token(type, literal, line, col) {
    return { type: type, literal: literal, line: line, col: col };
  }

  function tokenToString(t) {
    return (
      "Token{" +
      tokenName(t.type) +
      " " +
      JSON.stringify(t.literal) +
      " L" +
      t.line +
      ":C" +
      t.col +
      "}"
    );
  }

  // =====================================================================
  // Lexer
  // =====================================================================
  var KEYWORDS = {
    include: TOKEN.INCLUDE,
    extends: TOKEN.EXTENDS,
    true: TOKEN.BOOL,
    false: TOKEN.BOOL,
  };

  function isLetter(ch) {
    return (
      (ch >= "a" && ch <= "z") ||
      (ch >= "A" && ch <= "Z") ||
      ch === "_" ||
      (ch > "\x7f" && /\p{L}/u.test(ch))
    );
  }

  function isDigit(ch) {
    return ch >= "0" && ch <= "9";
  }

  function isIdentStart(ch) {
    return isLetter(ch);
  }

  function isIdentContinue(ch) {
    return isLetter(ch) || isDigit(ch) || ch === "_" || ch === "-" || ch === ".";
  }

  function Lexer(src) {
    this.source = src;
    this.pos = 0;
    this.line = 1;
    this.col = 1;
    this.tokens = [];
    this.indents = [0];
  }

  Lexer.prototype.peek = function () {
    if (this.pos >= this.source.length) return "\0";
    return this.source[this.pos];
  };

  Lexer.prototype.peekAt = function (offset) {
    var idx = this.pos + offset;
    if (idx >= this.source.length) return "\0";
    return this.source[idx];
  };

  Lexer.prototype.advance = function () {
    var ch = this.source[this.pos];
    this.pos++;
    if (ch === "\n") {
      this.line++;
      this.col = 1;
    } else {
      this.col++;
    }
    return ch;
  };

  Lexer.prototype.isEOF = function () {
    return this.pos >= this.source.length;
  };

  Lexer.prototype.skipToEOL = function () {
    while (!this.isEOF() && this.peek() !== "\n") {
      this.advance();
    }
  };

  Lexer.prototype.emit = function (type, literal, line, col) {
    this.tokens.push(Token(type, literal, line, col));
  };

  Lexer.prototype.tokenise = function () {
    while (!this.isEOF()) {
      var err = this.scanLine();
      if (err) throw new Error(err);
    }
    while (this.indents.length > 1) {
      this.emit(TOKEN.DEDENT, "", this.line, this.col);
      this.indents.pop();
    }
    this.emit(TOKEN.EOF, "", this.line, this.col);
    return this.tokens;
  };

  Lexer.prototype.scanLine = function () {
    var startLine = this.line;
    var startCol = this.col;

    var indent = 0;
    while (!this.isEOF() && this.peek() === " ") {
      indent++;
      this.advance();
    }

    if (this.isEOF() || this.peek() === "\n" || this.peek() === "#") {
      this.skipToEOL();
      if (!this.isEOF() && this.peek() === "\n") {
        this.advance();
      }
      return null;
    }

    var current = this.indents[this.indents.length - 1];
    if (indent > current) {
      this.indents.push(indent);
      this.emit(TOKEN.INDENT, "", startLine, startCol);
    } else if (indent < current) {
      while (
        this.indents.length > 1 &&
        this.indents[this.indents.length - 1] > indent
      ) {
        this.emit(TOKEN.DEDENT, "", startLine, startCol);
        this.indents.pop();
      }
      if (this.indents[this.indents.length - 1] !== indent) {
        return "line " + this.line + ": inconsistent indentation";
      }
    }

    while (!this.isEOF() && this.peek() !== "\n") {
      var err = this.scanToken();
      if (err) return err;
    }
    this.emit(TOKEN.NEWLINE, "\n", this.line, this.col);
    if (!this.isEOF() && this.peek() === "\n") {
      this.advance();
    }
    return null;
  };

  Lexer.prototype.scanToken = function () {
    while (!this.isEOF() && this.peek() === " ") {
      this.advance();
    }
    if (this.isEOF() || this.peek() === "\n") return null;

    var line = this.line,
      col = this.col;
    var ch = this.peek();

    if (ch === "#") {
      this.skipToEOL();
      return null;
    }
    if (ch === ":") {
      this.advance();
      this.emit(TOKEN.COLON, ":", line, col);
      return null;
    }
    if (ch === "=") {
      this.advance();
      this.emit(TOKEN.EQUALS, "=", line, col);
      return null;
    }
    if (ch === "-" && this.peekAt(1) === ">") {
      this.advance();
      this.advance();
      this.emit(TOKEN.ARROW, "->", line, col);
      return null;
    }
    if (ch === "-") {
      var next = this.peekAt(1);
      if (next === " " || next === "\n" || next === "\0") {
        this.advance();
        this.emit(TOKEN.DASH, "-", line, col);
        return null;
      }
      if (isDigit(next)) {
        return this.scanNumber(line, col);
      }
      this.advance();
      this.emit(TOKEN.DASH, "-", line, col);
      return null;
    }
    if (ch === '"') {
      return this.scanString(line, col);
    }
    if (isDigit(ch)) {
      return this.scanNumber(line, col);
    }
    if (isIdentStart(ch)) {
      return this.scanIdent(line, col);
    }
    return (
      "line " + line + " col " + col + ': unexpected character "' + ch + '"'
    );
  };

  Lexer.prototype.scanString = function (line, col) {
    this.advance(); // consume opening "
    var sb = "";
    while (!this.isEOF() && this.peek() !== '"' && this.peek() !== "\n") {
      if (this.peek() === "\\") {
        this.advance();
        var esc = this.peek();
        if (esc === "n") sb += "\n";
        else if (esc === "t") sb += "\t";
        else if (esc === '"') sb += '"';
        else if (esc === "\\") sb += "\\";
        else sb += "\\" + esc;
        this.advance();
      } else {
        sb += this.peek();
        this.advance();
      }
    }
    if (this.isEOF() || this.peek() === "\n") {
      return "line " + line + " col " + col + ": unterminated string literal";
    }
    this.advance(); // consume closing "
    this.emit(TOKEN.STRING, sb, line, col);
    return null;
  };

  Lexer.prototype.scanNumber = function (line, col) {
    var sb = "";
    if (this.peek() === "-") {
      sb += this.peek();
      this.advance();
    }
    var dotCount = 0;
    var slashOrColon = false;
    while (!this.isEOF()) {
      var ch = this.peek();
      if (isDigit(ch)) {
        sb += ch;
        this.advance();
      } else if (ch === ".") {
        dotCount++;
        sb += ch;
        this.advance();
      } else if (ch === "/" || ch === ":") {
        slashOrColon = true;
        sb += ch;
        this.advance();
      } else {
        break;
      }
    }
    if (slashOrColon || dotCount > 1) {
      this.emit(TOKEN.IP_CIDR, sb, line, col);
    } else if (dotCount === 1) {
      this.emit(TOKEN.FLOAT, sb, line, col);
    } else {
      this.emit(TOKEN.INT, sb, line, col);
    }
    return null;
  };

  Lexer.prototype.scanIdent = function (line, col) {
    var sb = "";
    while (!this.isEOF() && isIdentContinue(this.peek())) {
      sb += this.peek();
      this.advance();
    }
    var kw = KEYWORDS[sb];
    if (kw !== undefined) {
      this.emit(kw, sb, line, col);
    } else {
      this.emit(TOKEN.IDENT, sb, line, col);
    }
    return null;
  };

  // =====================================================================
  // AST Nodes
  // =====================================================================
  function Pos(line, col) {
    return { line: line, col: col };
  }

  function FileNode(pos) {
    return { type: "File", includes: [], statements: [], pos: pos };
  }

  function IncludeNode(path, pos) {
    return { type: "Include", path: path, pos: pos };
  }

  function BlockNode(path, pos) {
    return {
      type: "Block",
      path: path,
      extendsTarget: "",
      body: [],
      pos: pos,
    };
  }

  function AssignmentNode(path, value, pos) {
    return { type: "Assignment", path: path, value: value, pos: pos };
  }

  function ListBlockNode(path, pos) {
    return { type: "ListBlock", path: path, items: [], pos: pos };
  }

  function ListItemNode(pos) {
    return { type: "ListItem", inline: null, fields: [], pos: pos };
  }

  function StringValue(v, pos) {
    return { type: "StringValue", v: v, pos: pos, goValue: v };
  }

  function IntValue(v, pos) {
    return { type: "IntValue", v: v, pos: pos, goValue: v };
  }

  function FloatValue(v, pos) {
    return { type: "FloatValue", v: v, pos: pos, goValue: v };
  }

  function BoolValue(v, pos) {
    return { type: "BoolValue", v: v, pos: pos, goValue: v };
  }

  function IPCIDRValue(v, pos) {
    return { type: "IPCIDRValue", v: v, pos: pos, goValue: v };
  }

  // =====================================================================
  // Parser
  // =====================================================================
  function Parser(tokens) {
    this.tokens = tokens;
    this.pos = 0;
    this.errors = [];
  }

  Parser.prototype.current = function () {
    if (this.pos >= this.tokens.length)
      return Token(TOKEN.EOF, "", 0, 0);
    return this.tokens[this.pos];
  };

  Parser.prototype.peekNext = function () {
    if (this.pos + 1 >= this.tokens.length)
      return Token(TOKEN.EOF, "", 0, 0);
    return this.tokens[this.pos + 1];
  };

  Parser.prototype.advance = function () {
    var tok = this.current();
    if (this.pos < this.tokens.length) this.pos++;
    return tok;
  };

  Parser.prototype.isEOF = function () {
    return this.current().type === TOKEN.EOF;
  };

  Parser.prototype.skipNewlines = function () {
    while (this.current().type === TOKEN.NEWLINE) this.advance();
  };

  Parser.prototype.consumeNewline = function () {
    if (this.current().type === TOKEN.NEWLINE) this.advance();
  };

  Parser.prototype.consumeDedent = function () {
    if (this.current().type === TOKEN.DEDENT) this.advance();
  };

  Parser.prototype.errorf = function (msg) {
    this.errors.push(new Error(msg));
  };

  Parser.prototype.posOf = function (tok) {
    return Pos(tok.line, tok.col);
  };

  Parser.prototype.parse = function () {
    var file = FileNode(this.posOf(this.current()));
    while (!this.isEOF()) {
      this.skipNewlines();
      if (this.isEOF()) break;
      var stmt = this.parseTopLevel();
      if (stmt === null) continue;
      if (stmt.type === "Include") {
        file.includes.push(stmt);
      } else {
        file.statements.push(stmt);
      }
    }
    return { file: file, errors: this.errors };
  };

  Parser.prototype.parseTopLevel = function () {
    var tok = this.current();
    if (tok.type === TOKEN.INCLUDE) return this.parseInclude();
    if (tok.type === TOKEN.IDENT) return this.parseStatement();
    if (tok.type === TOKEN.NEWLINE) {
      this.advance();
      return null;
    }
    this.errorf("unexpected token at top level: " + tokenToString(tok));
    this.advance();
    return null;
  };

  Parser.prototype.parseStatement = function () {
    var pos = this.posOf(this.current());
    var path = this.parsePath();
    var cur = this.current();

    if (cur.type === TOKEN.COLON) {
      return this.parseBlockOrList(path, pos);
    }
    if (cur.type === TOKEN.EQUALS) {
      this.advance(); // consume =
      var val = this.parseValue();
      this.consumeNewline();
      return AssignmentNode(path, val, pos);
    }
    if (cur.type === TOKEN.IDENT) {
      var name = cur.literal;
      this.advance();
      path = path.concat([name]);

      var extendsTarget = "";
      if (this.current().type === TOKEN.ARROW) {
        this.advance(); // consume ->
        if (this.current().literal !== "extends") {
          this.errorf(
            "expected 'extends' after ->, got " +
              JSON.stringify(this.current().literal)
          );
        } else {
          this.advance(); // consume 'extends'
          extendsTarget = this.current().literal;
          this.advance(); // consume target name
        }
      }

      if (this.current().type !== TOKEN.COLON) {
        this.errorf(
          "expected ':' to open block, got " + tokenToString(this.current())
        );
        return null;
      }
      var blk = this.parseBlockOrList(path, pos);
      if (blk && blk.type === "Block") {
        blk.extendsTarget = extendsTarget;
      }
      return blk;
    }

    this.errorf(
      "unexpected token after path [" +
        path.join(", ") +
        "]: " +
        tokenToString(cur)
    );
    this.advance();
    return null;
  };

  Parser.prototype.parsePath = function () {
    var path = [];
    if (this.current().type !== TOKEN.IDENT) return path;
    path.push(this.current().literal);
    this.advance();

    while (this.current().type === TOKEN.ARROW) {
      this.advance(); // consume ->
      if (this.current().literal === "extends") break;
      if (this.current().type !== TOKEN.IDENT) {
        this.errorf(
          "expected identifier after ->, got " + tokenToString(this.current())
        );
        break;
      }
      path.push(this.current().literal);
      this.advance();
    }
    return path;
  };

  Parser.prototype.parseBlockOrList = function (path, pos) {
    this.advance(); // consume ':'
    this.consumeNewline();
    this.skipNewlines();

    if (this.current().type !== TOKEN.INDENT) {
      return BlockNode(path, pos);
    }
    this.advance(); // consume INDENT
    this.skipNewlines();

    if (this.current().type === TOKEN.DASH) {
      return this.parseListBody(path, pos);
    }
    return this.parseBlockBody(path, pos);
  };

  Parser.prototype.parseBlockBody = function (path, pos) {
    var blk = BlockNode(path, pos);
    while (!this.isEOF() && this.current().type !== TOKEN.DEDENT) {
      this.skipNewlines();
      if (this.current().type === TOKEN.DEDENT || this.isEOF()) break;
      var stmt = this.parseStatement();
      if (stmt !== null) blk.body.push(stmt);
    }
    this.consumeDedent();
    return blk;
  };

  Parser.prototype.parseListBody = function (path, pos) {
    var lb = ListBlockNode(path, pos);
    while (!this.isEOF() && this.current().type !== TOKEN.DEDENT) {
      this.skipNewlines();
      if (this.current().type !== TOKEN.DASH) break;
      lb.items.push(this.parseListItem());
    }
    this.consumeDedent();
    return lb;
  };

  Parser.prototype.parseListItem = function () {
    var pos = this.posOf(this.current());
    this.advance(); // consume '-'
    var item = ListItemNode(pos);

    var isStructured =
      this.current().type === TOKEN.IDENT &&
      this.peekNext().type === TOKEN.EQUALS;

    if (isStructured) {
      while (
        this.current().type === TOKEN.IDENT &&
        this.peekNext().type === TOKEN.EQUALS
      ) {
        var stmtPos = this.posOf(this.current());
        var key = this.current().literal;
        this.advance(); // key
        this.advance(); // =
        var val = this.parseValue();
        item.fields.push(AssignmentNode([key], val, stmtPos));
      }
      this.consumeNewline();

      if (this.current().type === TOKEN.INDENT) {
        this.advance(); // consume INDENT
        while (this.current().type !== TOKEN.DEDENT && !this.isEOF()) {
          this.skipNewlines();
          if (this.current().type === TOKEN.DEDENT || this.isEOF()) break;
          var stmt = this.parseStatement();
          if (stmt !== null) item.fields.push(stmt);
        }
        this.consumeDedent();
      }
    } else {
      item.inline = this.parseValue();
      this.consumeNewline();
    }
    return item;
  };

  Parser.prototype.parseValue = function () {
    var tok = this.current();
    var pos = this.posOf(tok);

    if (tok.type === TOKEN.STRING) {
      this.advance();
      return StringValue(tok.literal, pos);
    }
    if (tok.type === TOKEN.INT) {
      this.advance();
      var iv = parseInt(tok.literal, 10);
      return IntValue(iv, pos);
    }
    if (tok.type === TOKEN.FLOAT) {
      this.advance();
      var fv = parseFloat(tok.literal);
      return FloatValue(fv, pos);
    }
    if (tok.type === TOKEN.BOOL) {
      this.advance();
      return BoolValue(tok.literal.toLowerCase() === "true", pos);
    }
    if (tok.type === TOKEN.IP_CIDR) {
      this.advance();
      return IPCIDRValue(tok.literal, pos);
    }
    if (tok.type === TOKEN.IDENT) {
      this.advance();
      return StringValue(tok.literal, pos);
    }
    this.errorf("expected a value, got " + tokenToString(tok));
    this.advance();
    return StringValue("", pos);
  };

  Parser.prototype.parseInclude = function () {
    var pos = this.posOf(this.current());
    this.advance(); // consume 'include'
    var pathTok = this.current();
    if (pathTok.type !== TOKEN.STRING && pathTok.type !== TOKEN.IDENT) {
      this.errorf(
        "expected file path after include, got " + tokenToString(pathTok)
      );
      return null;
    }
    this.advance();
    this.consumeNewline();
    return IncludeNode(pathTok.literal, pos);
  };

  // =====================================================================
  // Evaluator
  // =====================================================================
  function Evaluator(includeResolver) {
    this.includeResolver = includeResolver || null;
    this.namedBlocks = {};
  }

  Evaluator.prototype.evaluate = function (file) {
    var result = {};

    // 1. Process includes
    if (this.includeResolver) {
      for (var i = 0; i < file.includes.length; i++) {
        var cfg = this.includeResolver(file.includes[i].path);
        if (cfg) mergeConfig(result, cfg);
      }
    }

    // 2. Register named two-segment blocks
    for (var i = 0; i < file.statements.length; i++) {
      var stmt = file.statements[i];
      if (stmt.type === "Block" && stmt.path.length >= 2) {
        var key = stmt.path[0] + ":" + stmt.path[1];
        this.namedBlocks[key] = stmt;
      }
    }

    // 3. Evaluate all statements
    for (var i = 0; i < file.statements.length; i++) {
      this.evalStatement(result, file.statements[i]);
    }
    return result;
  };

  Evaluator.prototype.evaluateSource = function (src) {
    var lexer = new Lexer(src);
    var tokens = lexer.tokenise();
    var parser = new Parser(tokens);
    var result = parser.parse();
    if (result.errors.length > 0) {
      throw new Error("parse errors: " + result.errors.join("; "));
    }
    return this.evaluate(result.file);
  };

  Evaluator.prototype.evalStatement = function (cfg, stmt) {
    if (stmt.type === "Block") return this.evalBlock(cfg, stmt);
    if (stmt.type === "Assignment") return this.evalAssignment(cfg, stmt);
    if (stmt.type === "ListBlock") return this.evalListBlock(cfg, stmt);
  };

  Evaluator.prototype.evalBlock = function (cfg, blk) {
    var target = cloneConfig(getNestedConfig(cfg, blk.path));

    if (blk.extendsTarget) {
      var base = this.resolveExtends(blk);
      if (base) mergeConfig(target, base);
    }

    for (var i = 0; i < blk.body.length; i++) {
      this.evalStatement(target, blk.body[i]);
    }

    setNested(cfg, blk.path, target);
  };

  Evaluator.prototype.evalAssignment = function (cfg, a) {
    setNested(cfg, a.path, a.value.goValue);
  };

  Evaluator.prototype.evalListBlock = function (cfg, lb) {
    var items = [];
    for (var i = 0; i < lb.items.length; i++) {
      var item = lb.items[i];
      if (item.inline !== null) {
        items.push(item.inline.goValue);
      } else {
        var sub = {};
        for (var j = 0; j < item.fields.length; j++) {
          this.evalStatement(sub, item.fields[j]);
        }
        items.push(sub);
      }
    }
    setNested(cfg, lb.path, items);
  };

  Evaluator.prototype.resolveExtends = function (blk) {
    if (blk.path.length < 1) return null;
    var kind = blk.path[0];
    var target = blk.extendsTarget;
    var key = kind + ":" + target;

    var baseBlk = this.namedBlocks[key];
    if (!baseBlk) {
      throw new Error(
        'extends: cannot find "' + target + '" (key "' + key + '" not registered)'
      );
    }

    var extendingName = blk.path[blk.path.length - 1];
    if (baseBlk.extendsTarget === extendingName) {
      throw new Error(
        "extends cycle detected between " +
          JSON.stringify(blk.path) +
          " and " +
          JSON.stringify(baseBlk.path)
      );
    }

    var tempRoot = {};
    this.evalBlock(tempRoot, baseBlk);
    return getNestedConfig(tempRoot, baseBlk.path);
  };

  // Config helpers
  function getNestedConfig(cfg, path) {
    var cur = cfg;
    for (var i = 0; i < path.length; i++) {
      if (cur === null || cur === undefined || typeof cur !== "object" || Array.isArray(cur))
        return {};
      cur = cur[path[i]];
    }
    if (cur !== null && cur !== undefined && typeof cur === "object" && !Array.isArray(cur))
      return cur;
    return {};
  }

  function setNested(cfg, path, value) {
    if (path.length === 0) return;
    if (path.length === 1) {
      cfg[path[0]] = value;
      return;
    }
    var seg = path[0];
    var sub;
    if (
      cfg[seg] !== undefined &&
      cfg[seg] !== null &&
      typeof cfg[seg] === "object" &&
      !Array.isArray(cfg[seg])
    ) {
      sub = cfg[seg];
    } else {
      sub = {};
    }
    cfg[seg] = sub;
    setNested(sub, path.slice(1), value);
  }

  function mergeConfig(dst, src) {
    for (var k in src) {
      if (!src.hasOwnProperty(k)) continue;
      var sv = src[k];
      var dv = dst[k];
      if (
        dv !== undefined &&
        typeof dv === "object" &&
        !Array.isArray(dv) &&
        typeof sv === "object" &&
        !Array.isArray(sv) &&
        sv !== null
      ) {
        mergeConfig(dv, sv);
      } else {
        dst[k] = sv;
      }
    }
  }

  function cloneConfig(src) {
    var dst = {};
    for (var k in src) {
      if (src.hasOwnProperty(k)) dst[k] = src[k];
    }
    return dst;
  }

  // =====================================================================
  // Emitter - JSON
  // =====================================================================
  function toJSON(cfg) {
    return JSON.stringify(cfg, null, 2);
  }

  // =====================================================================
  // Emitter - ENV
  // =====================================================================
  function toENV(cfg, prefix) {
    var lines = [];
    prefix = prefix ? prefix.toUpperCase() : "";
    collectENV(cfg, prefix, lines);
    lines.sort();
    return lines.join("\n");
  }

  function collectENV(cfg, prefix, lines) {
    for (var k in cfg) {
      if (!cfg.hasOwnProperty(k)) continue;
      var v = cfg[k];
      var key = envKey(prefix, k);
      if (v !== null && typeof v === "object" && !Array.isArray(v)) {
        collectENV(v, key, lines);
      } else if (Array.isArray(v)) {
        lines.push(key + "=" + sliceToEnv(v));
      } else {
        lines.push(key + "=" + v);
      }
    }
  }

  function envKey(prefix, key) {
    var k = key.toUpperCase().replace(/-/g, "_");
    if (!prefix) return k;
    return prefix + "_" + k;
  }

  function sliceToEnv(items) {
    var parts = [];
    for (var i = 0; i < items.length; i++) {
      var item = items[i];
      if (item !== null && typeof item === "object" && !Array.isArray(item)) {
        parts.push(JSON.stringify(item));
      } else {
        parts.push("" + item);
      }
    }
    return parts.join(",");
  }

  // =====================================================================
  // Emitter - Netio (canonical DSL)
  // =====================================================================
  function toNetio(cfg) {
    var sb = [];
    writeConfig(sb, cfg, 0);
    return sb.join("");
  }

  function writeConfig(sb, cfg, depth) {
    var indent = repeat("  ", depth);
    var keys = Object.keys(cfg).sort();

    for (var i = 0; i < keys.length; i++) {
      var k = keys[i];
      var v = cfg[k];
      if (v !== null && typeof v === "object" && !Array.isArray(v)) {
        sb.push(indent + k + ":\n");
        writeConfig(sb, v, depth + 1);
      } else if (Array.isArray(v)) {
        sb.push(indent + k + ":\n");
        for (var j = 0; j < v.length; j++) {
          writeListItem(sb, v[j], depth + 1);
        }
      } else {
        sb.push(indent + k + " = " + formatScalar(v) + "\n");
      }
    }
  }

  function writeListItem(sb, item, depth) {
    var indent = repeat("  ", depth);
    if (item !== null && typeof item === "object" && !Array.isArray(item)) {
      sb.push(indent + "-");
      var keys = Object.keys(item).sort();
      var first = true;
      for (var i = 0; i < keys.length; i++) {
        var k = keys[i];
        var v = item[k];
        if (
          v !== null &&
          typeof v === "object"
        ) {
          if (first) {
            sb.push("\n");
            first = false;
          }
          if (Array.isArray(v)) {
            sb.push(indent + "  " + k + ":\n");
            for (var j = 0; j < v.length; j++) {
              writeListItem(sb, v[j], depth + 2);
            }
          } else {
            sb.push(indent + "  " + k + ":\n");
            writeConfig(sb, v, depth + 2);
          }
        } else {
          if (first) {
            sb.push(" " + k + " = " + formatScalar(v));
            first = false;
          } else {
            sb.push("\n" + indent + "  " + k + " = " + formatScalar(v));
          }
        }
      }
      sb.push("\n");
    } else {
      sb.push(indent + "- " + formatScalar(item) + "\n");
    }
  }

  function formatScalar(v) {
    if (typeof v === "string") {
      if (/[\s"]/.test(v)) return JSON.stringify(v);
      return v;
    }
    if (typeof v === "boolean") return v ? "true" : "false";
    return "" + v;
  }

  function repeat(s, n) {
    var r = "";
    for (var i = 0; i < n; i++) r += s;
    return r;
  }

  // =====================================================================
  // Schema Validation
  // =====================================================================
  function ValidationError(path, message) {
    this.path = path;
    this.message = message;
  }

  ValidationError.prototype.toString = function () {
    if (!this.path) return this.message;
    return this.path + ": " + this.message;
  };

  function ValidationErrors(errs) {
    this.errors = errs || [];
  }

  ValidationErrors.prototype.hasErrors = function () {
    return this.errors.length > 0;
  };

  ValidationErrors.prototype.toString = function () {
    return (
      "validation failed:\n" +
      this.errors.map(function (e) { return "  - " + e.toString(); }).join("\n")
    );
  };

  function schemaJoinPath(parent, child) {
    if (!parent) return child;
    return parent + "." + child;
  }

  // Schema (root)
  function Schema() {
    this._fields = {};
    this._required = [];
  }

  Schema.prototype.required = function () {
    for (var i = 0; i < arguments.length; i++) {
      this._required.push(arguments[i]);
    }
    return this;
  };

  Schema.prototype.field = function (key, v) {
    this._fields[key] = v;
    return this;
  };

  Schema.prototype.validate = function (cfg) {
    var errs = validateObject(this._fields, this._required, "", cfg);
    return new ValidationErrors(errs);
  };

  function validateObject(fields, required, path, value) {
    var errs = [];
    var m = value;
    if (value === null || value === undefined) m = {};
    if (typeof m !== "object" || Array.isArray(m)) {
      errs.push(
        new ValidationError(
          path,
          "expected an object, got " + typeof value
        )
      );
      return errs;
    }

    for (var i = 0; i < required.length; i++) {
      var rk = required[i];
      if (!(rk in m)) {
        errs.push(
          new ValidationError(
            schemaJoinPath(path, rk),
            "required field is missing"
          )
        );
      }
    }

    for (var key in fields) {
      if (!fields.hasOwnProperty(key)) continue;
      var v = m[key] !== undefined ? m[key] : null;
      var fieldErrs = fields[key].validate(schemaJoinPath(path, key), v);
      errs = errs.concat(fieldErrs);
    }
    return errs;
  }

  // ObjectValidator
  function ObjectValidator() {
    this._fields = {};
    this._required = [];
    this._optional = false;
  }

  ObjectValidator.prototype.required = function () {
    for (var i = 0; i < arguments.length; i++) {
      this._required.push(arguments[i]);
    }
    return this;
  };

  ObjectValidator.prototype.field = function (key, v) {
    this._fields[key] = v;
    return this;
  };

  ObjectValidator.prototype.optional = function () {
    this._optional = true;
    return this;
  };

  ObjectValidator.prototype.validate = function (path, value) {
    if (value === null || value === undefined) {
      if (this._optional) return [];
    }
    return validateObject(this._fields, this._required, path, value);
  };

  // StringValidator
  function StringValidator() {
    this._nonEmpty = false;
    this._minLen = null;
    this._maxLen = null;
    this._oneOf = null;
    this._optional = false;
  }

  StringValidator.prototype.nonEmpty = function () {
    this._nonEmpty = true;
    return this;
  };
  StringValidator.prototype.minLen = function (n) {
    this._minLen = n;
    return this;
  };
  StringValidator.prototype.maxLen = function (n) {
    this._maxLen = n;
    return this;
  };
  StringValidator.prototype.oneOf = function () {
    this._oneOf = Array.prototype.slice.call(arguments);
    return this;
  };
  StringValidator.prototype.optional = function () {
    this._optional = true;
    return this;
  };

  StringValidator.prototype.validate = function (path, value) {
    var errs = [];
    if (value === null || value === undefined) return errs;
    if (typeof value !== "string") {
      errs.push(
        new ValidationError(path, "expected string, got " + typeof value)
      );
      return errs;
    }
    if (this._nonEmpty && value.trim() === "") {
      errs.push(new ValidationError(path, "value must not be empty"));
    }
    if (this._minLen !== null && value.length < this._minLen) {
      errs.push(
        new ValidationError(
          path,
          "string length " +
            value.length +
            " is below minimum " +
            this._minLen
        )
      );
    }
    if (this._maxLen !== null && value.length > this._maxLen) {
      errs.push(
        new ValidationError(
          path,
          "string length " +
            value.length +
            " exceeds maximum " +
            this._maxLen
        )
      );
    }
    if (this._oneOf !== null) {
      if (this._oneOf.indexOf(value) === -1) {
        errs.push(
          new ValidationError(
            path,
            'value "' +
              value +
              '" is not one of [' +
              this._oneOf.join(", ") +
              "]"
          )
        );
      }
    }
    return errs;
  };

  // IntValidator
  function IntValidator() {
    this._min = null;
    this._max = null;
    this._optional = false;
  }

  IntValidator.prototype.min = function (n) {
    this._min = n;
    return this;
  };
  IntValidator.prototype.max = function (n) {
    this._max = n;
    return this;
  };
  IntValidator.prototype.optional = function () {
    this._optional = true;
    return this;
  };

  IntValidator.prototype.validate = function (path, value) {
    var errs = [];
    if (value === null || value === undefined) return errs;
    if (typeof value !== "number") {
      errs.push(
        new ValidationError(path, "expected integer, got " + typeof value)
      );
      return errs;
    }
    var n = value;
    if (this._min !== null && n < this._min) {
      errs.push(
        new ValidationError(
          path,
          "value " + n + " is below minimum " + this._min
        )
      );
    }
    if (this._max !== null && n > this._max) {
      errs.push(
        new ValidationError(
          path,
          "value " + n + " exceeds maximum " + this._max
        )
      );
    }
    return errs;
  };

  // FloatValidator
  function FloatValidator() {
    this._min = null;
    this._max = null;
    this._optional = false;
  }

  FloatValidator.prototype.min = function (n) {
    this._min = n;
    return this;
  };
  FloatValidator.prototype.max = function (n) {
    this._max = n;
    return this;
  };
  FloatValidator.prototype.optional = function () {
    this._optional = true;
    return this;
  };

  FloatValidator.prototype.validate = function (path, value) {
    var errs = [];
    if (value === null || value === undefined) return errs;
    if (typeof value !== "number") {
      errs.push(
        new ValidationError(path, "expected float, got " + typeof value)
      );
      return errs;
    }
    if (this._min !== null && value < this._min) {
      errs.push(
        new ValidationError(
          path,
          "value " + value + " is below minimum " + this._min
        )
      );
    }
    if (this._max !== null && value > this._max) {
      errs.push(
        new ValidationError(
          path,
          "value " + value + " exceeds maximum " + this._max
        )
      );
    }
    return errs;
  };

  // BoolValidator
  function BoolValidator() {}

  BoolValidator.prototype.validate = function (path, value) {
    if (value === null || value === undefined) return [];
    if (typeof value !== "boolean") {
      return [
        new ValidationError(path, "expected bool, got " + typeof value),
      ];
    }
    return [];
  };

  // ListValidator
  function ListValidator() {
    this._minItems = null;
    this._maxItems = null;
    this._itemV = null;
    this._optional = false;
  }

  ListValidator.prototype.minItems = function (n) {
    this._minItems = n;
    return this;
  };
  ListValidator.prototype.maxItems = function (n) {
    this._maxItems = n;
    return this;
  };
  ListValidator.prototype.items = function (v) {
    this._itemV = v;
    return this;
  };
  ListValidator.prototype.optional = function () {
    this._optional = true;
    return this;
  };

  ListValidator.prototype.validate = function (path, value) {
    var errs = [];
    if (value === null || value === undefined) return errs;
    if (!Array.isArray(value)) {
      errs.push(
        new ValidationError(path, "expected list, got " + typeof value)
      );
      return errs;
    }
    if (this._minItems !== null && value.length < this._minItems) {
      errs.push(
        new ValidationError(
          path,
          "list has " +
            value.length +
            " items, minimum is " +
            this._minItems
        )
      );
    }
    if (this._maxItems !== null && value.length > this._maxItems) {
      errs.push(
        new ValidationError(
          path,
          "list has " +
            value.length +
            " items, maximum is " +
            this._maxItems
        )
      );
    }
    if (this._itemV !== null) {
      for (var i = 0; i < value.length; i++) {
        var itemPath = path + "[" + i + "]";
        errs = errs.concat(this._itemV.validate(itemPath, value[i]));
      }
    }
    return errs;
  };

  // AnyValidator
  function AnyValidator() {}
  AnyValidator.prototype.validate = function () {
    return [];
  };

  // =====================================================================
  // Linter
  // =====================================================================
  var SEVERITY_WARNING = 0;
  var SEVERITY_ERROR = 1;

  function LintIssue(rule, severity, pos, message) {
    return { rule: rule, severity: severity, pos: pos, message: message };
  }

  function lintIssueToString(i) {
    var sev = i.severity === SEVERITY_ERROR ? "ERROR" : "WARN";
    return (
      "[" +
      sev +
      "] " +
      i.rule +
      " at L" +
      i.pos.line +
      ":C" +
      i.pos.col +
      ": " +
      i.message
    );
  }

  function LinterConfig(opts) {
    opts = opts || {};
    this.checkDuplicateKeys =
      opts.checkDuplicateKeys !== undefined ? opts.checkDuplicateKeys : true;
    this.checkEmptyBlocks =
      opts.checkEmptyBlocks !== undefined ? opts.checkEmptyBlocks : true;
    this.checkUnsortedKeys =
      opts.checkUnsortedKeys !== undefined ? opts.checkUnsortedKeys : true;
    this.checkDeepNesting =
      opts.checkDeepNesting !== undefined ? opts.checkDeepNesting : true;
    this.maxNestingDepth =
      opts.maxNestingDepth !== undefined ? opts.maxNestingDepth : 6;
  }

  function LinterObj(cfg) {
    this.cfg = cfg || new LinterConfig();
  }

  LinterObj.prototype.lint = function (file) {
    var issues = [];
    for (var i = 0; i < file.statements.length; i++) {
      issues = issues.concat(this.lintStatement(file.statements[i], 0));
    }
    return issues;
  };

  LinterObj.prototype.lintStatement = function (stmt, depth) {
    if (stmt.type === "Block") return this.lintBlock(stmt, depth);
    if (stmt.type === "ListBlock") return this.lintListBlock(stmt);
    return [];
  };

  LinterObj.prototype.lintBlock = function (blk, depth) {
    var issues = [];

    if (this.cfg.checkDeepNesting && depth >= this.cfg.maxNestingDepth) {
      issues.push(
        LintIssue(
          "DeepNesting",
          SEVERITY_WARNING,
          blk.pos,
          "block [" +
            blk.path.join(", ") +
            "] is nested " +
            (depth + 1) +
            " levels deep (max " +
            this.cfg.maxNestingDepth +
            ")"
        )
      );
    }

    if (this.cfg.checkEmptyBlocks && blk.body.length === 0) {
      issues.push(
        LintIssue(
          "EmptyBlock",
          SEVERITY_WARNING,
          blk.pos,
          'block "' + blk.path.join(" -> ") + '" has no body'
        )
      );
    }

    if (blk.body.length > 0) {
      if (this.cfg.checkDuplicateKeys) {
        issues = issues.concat(this.checkDuplicateKeys(blk));
      }
      if (this.cfg.checkUnsortedKeys) {
        issues = issues.concat(this.checkUnsortedKeys(blk));
      }
      for (var i = 0; i < blk.body.length; i++) {
        issues = issues.concat(this.lintStatement(blk.body[i], depth + 1));
      }
    }
    return issues;
  };

  LinterObj.prototype.checkDuplicateKeys = function (blk) {
    var issues = [];
    var seen = {};
    for (var i = 0; i < blk.body.length; i++) {
      var stmt = blk.body[i];
      var key = null;
      if (stmt.type === "Assignment") key = stmt.path.join("->");
      else if (stmt.type === "Block") key = stmt.path.join("->");
      else if (stmt.type === "ListBlock") key = stmt.path.join("->");
      if (!key) continue;
      if (seen[key]) {
        issues.push(
          LintIssue(
            "DuplicateKey",
            SEVERITY_ERROR,
            stmt.pos,
            'key "' +
              key +
              '" is defined more than once in block [' +
              blk.path.join(", ") +
              "]"
          )
        );
      }
      seen[key] = true;
    }
    return issues;
  };

  LinterObj.prototype.checkUnsortedKeys = function (blk) {
    var issues = [];
    var keys = [];
    var keyPos = {};
    for (var i = 0; i < blk.body.length; i++) {
      var stmt = blk.body[i];
      if (stmt.type === "Assignment") {
        var k = stmt.path.join("->");
        keys.push(k);
        keyPos[k] = stmt.pos;
      }
    }
    if (keys.length <= 1) return [];
    var sorted = keys.slice().sort();
    for (var i = 0; i < keys.length; i++) {
      if (keys[i] !== sorted[i]) {
        issues.push(
          LintIssue(
            "UnsortedKeys",
            SEVERITY_WARNING,
            keyPos[keys[i]],
            'key "' +
              keys[i] +
              '" is out of canonical order (expected "' +
              sorted[i] +
              '" at position ' +
              (i + 1) +
              ")"
          )
        );
        break;
      }
    }
    return issues;
  };

  LinterObj.prototype.lintListBlock = function (lb) {
    var issues = [];
    if (this.cfg.checkEmptyBlocks && lb.items.length === 0) {
      issues.push(
        LintIssue(
          "EmptyBlock",
          SEVERITY_WARNING,
          lb.pos,
          'list "' + lb.path.join(" -> ") + '" has no items'
        )
      );
    }
    return issues;
  };

  function issuesHasErrors(issues) {
    for (var i = 0; i < issues.length; i++) {
      if (issues[i].severity === SEVERITY_ERROR) return true;
    }
    return false;
  }

  // =====================================================================
  // Diff
  // =====================================================================
  var CHANGE_ADDED = 0;
  var CHANGE_REMOVED = 1;
  var CHANGE_MODIFIED = 2;

  function Change(kind, path, oldValue, newValue) {
    return { kind: kind, path: path, oldValue: oldValue, newValue: newValue };
  }

  function diffCompare(oldCfg, newCfg) {
    var changes = [];
    diffCompareInner("", oldCfg, newCfg, changes);
    changes.sort(function (a, b) {
      return a.path < b.path ? -1 : a.path > b.path ? 1 : 0;
    });
    return changes;
  }

  function diffCompareInner(prefix, oldCfg, newCfg, out) {
    for (var k in oldCfg) {
      if (!oldCfg.hasOwnProperty(k)) continue;
      var path = diffJoinPath(prefix, k);
      var ov = oldCfg[k];
      if (!(k in newCfg)) {
        out.push(Change(CHANGE_REMOVED, path, ov, null));
        continue;
      }
      diffCompareValues(path, ov, newCfg[k], out);
    }
    for (var k in newCfg) {
      if (!newCfg.hasOwnProperty(k)) continue;
      if (!(k in oldCfg)) {
        out.push(Change(CHANGE_ADDED, diffJoinPath(prefix, k), null, newCfg[k]));
      }
    }
  }

  function diffCompareValues(path, ov, nv, out) {
    var oldIsObj = isConfigObj(ov);
    var newIsObj = isConfigObj(nv);

    if (oldIsObj && newIsObj) {
      diffCompareInner(path, ov, nv, out);
    } else if (Array.isArray(ov) && Array.isArray(nv)) {
      if (!diffSlicesEqual(ov, nv)) {
        out.push(Change(CHANGE_MODIFIED, path, ov, nv));
      }
    } else {
      if ("" + ov !== "" + nv) {
        out.push(Change(CHANGE_MODIFIED, path, ov, nv));
      }
    }
  }

  function isConfigObj(v) {
    return v !== null && v !== undefined && typeof v === "object" && !Array.isArray(v);
  }

  function diffSlicesEqual(a, b) {
    if (a.length !== b.length) return false;
    for (var i = 0; i < a.length; i++) {
      if ("" + a[i] !== "" + b[i]) return false;
    }
    return true;
  }

  function diffJoinPath(parent, child) {
    if (!parent) return child;
    return parent + "." + child;
  }

  function diffFormatVal(v) {
    if (v === null || v === undefined) return "<nil>";
    if (typeof v === "string") return JSON.stringify(v);
    if (Array.isArray(v)) {
      return "[" + v.map(function (item) { return "" + item; }).join(", ") + "]";
    }
    if (typeof v === "object") return "{...}";
    return "" + v;
  }

  function diffRender(changes) {
    if (changes.length === 0) return "(no differences)";
    return changes
      .map(function (c) {
        if (c.kind === CHANGE_ADDED)
          return "+ " + c.path + " = " + diffFormatVal(c.newValue);
        if (c.kind === CHANGE_REMOVED)
          return "- " + c.path + " = " + diffFormatVal(c.oldValue);
        return (
          "~ " +
          c.path +
          ": " +
          diffFormatVal(c.oldValue) +
          " \u2192 " +
          diffFormatVal(c.newValue)
        );
      })
      .join("\n");
  }

  function diffSummary(changes) {
    var added = 0,
      removed = 0,
      modified = 0;
    for (var i = 0; i < changes.length; i++) {
      if (changes[i].kind === CHANGE_ADDED) added++;
      else if (changes[i].kind === CHANGE_REMOVED) removed++;
      else modified++;
    }
    var parts = [];
    if (added > 0) parts.push(added + " added");
    if (removed > 0) parts.push(removed + " removed");
    if (modified > 0) parts.push(modified + " modified");
    if (parts.length === 0) return "no differences";
    return parts.join(", ");
  }

  // =====================================================================
  // Public API
  // =====================================================================
  return {
    // Constants
    TOKEN: TOKEN,
    TOKEN_NAMES: TOKEN_NAMES,
    SEVERITY_WARNING: SEVERITY_WARNING,
    SEVERITY_ERROR: SEVERITY_ERROR,
    CHANGE_ADDED: CHANGE_ADDED,
    CHANGE_REMOVED: CHANGE_REMOVED,
    CHANGE_MODIFIED: CHANGE_MODIFIED,

    // Lexer
    tokenise: function (src) {
      return new Lexer(src).tokenise();
    },
    tokenName: tokenName,
    tokenToString: tokenToString,

    // Parser
    parseAST: function (src) {
      var tokens = new Lexer(src).tokenise();
      var p = new Parser(tokens);
      return p.parse();
    },

    // Evaluator
    parseString: function (src, includeResolver) {
      var ev = new Evaluator(includeResolver);
      return ev.evaluateSource(src);
    },

    // Emitters
    toJSON: toJSON,
    toENV: toENV,
    toNetio: toNetio,

    // Schema
    schema: function () {
      return new Schema();
    },
    object: function () {
      return new ObjectValidator();
    },
    str: function () {
      return new StringValidator();
    },
    int: function () {
      return new IntValidator();
    },
    float: function () {
      return new FloatValidator();
    },
    bool: function () {
      return new BoolValidator();
    },
    list: function () {
      return new ListValidator();
    },
    any: function () {
      return new AnyValidator();
    },

    // Linter
    lint: function (src, config) {
      var result = Netio.parseAST(src);
      if (result.errors.length > 0) throw result.errors[0];
      var l = new LinterObj(config ? new LinterConfig(config) : new LinterConfig());
      return l.lint(result.file);
    },
    lintAST: function (file, config) {
      var l = new LinterObj(config ? new LinterConfig(config) : new LinterConfig());
      return l.lint(file);
    },
    lintIssueToString: lintIssueToString,
    issuesHasErrors: issuesHasErrors,
    LinterConfig: LinterConfig,

    // Diff
    diff: diffCompare,
    diffRender: diffRender,
    diffSummary: diffSummary,

    // Internal (for testing)
    _Lexer: Lexer,
    _Parser: Parser,
    _Evaluator: Evaluator,
  };
})();

if (typeof module !== "undefined" && module.exports) {
  module.exports = Netio;
}
