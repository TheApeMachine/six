/**
 * Tree-sitter grammar for Six feed programs (pipes, operations, emit wrappers).
 */
/// <reference types="tree-sitter-cli/dsl" />
// @ts-check

module.exports = grammar({
  name: 'six',

  extras: ($) => [/[\s\uFEFF]/, $.line_comment],

  conflicts: ($) => [[$.emit_block]],

  rules: {
    source_file: ($) => repeat(choice($.feed, $._site)),

    line_comment: ($) =>
      token(choice(seq(';', /[^\r\n]*/), seq('#', /[^\r\n]*/))),

    /* Feed bond between bracket sites (see compiler compileFeedSource). */
    feed: ($) => '<=',

    _site: ($) => choice($.emit_block, $.pipe),

    /*
     * One < prefix; pipes may be separated by <= inside the block (see structural feed tests).
     * Trailing optional > closes emit; otherwise emit stays sticky until the block ends.
     */
    emit_block: ($) =>
      seq('<', $.pipe, repeat(seq(optional($.feed), $.pipe)), optional('>')),

    pipe: ($) =>
      seq(
        '[',
        optional(choice($.pipe_body_compact, repeat1($._term))),
        ']',
      ),

    pipe_body_compact: ($) => seq('(', repeat($._term), ')'),

    operation: ($) => seq('{', repeat($._term), '}'),

    _term: ($) =>
      choice($.call, $.owner, $.ref, $.number, $.operator, $.reducer, $.topology, $.operation, $.question),

    call: ($) => seq($.owner, '(', $.ref, optional($.rotation), ')'),

    rotation: ($) => seq($.number, $.shift),

    owner: ($) => token(prec(3, choice('A', 'B'))),

    ref: ($) =>
      token(
        prec(
          1,
          choice(
            seq(optional('*'), /[0-9]+\.\.[0-9]+/),
            seq(
              optional('*'),
              /[a-zA-Z_][a-zA-Z0-9_.]*/,
              optional(seq('[', /\s*[0-9]+\s*/, optional(seq(',', /\s*[0-9]+\s*/)), ']')),
            ),
          ),
        ),
      ),

    reducer: ($) => token(prec(2, choice('popcnt', 'any_zero', 'all_ones'))),

    topology: ($) => token(prec(2, choice('self', 'next', 'fold', 'spawn', 'emit'))),

    question: ($) => '?',

    number: ($) => /[0-9]+(\.[0-9]+)?/,

    shift: ($) => token(choice('<<', '>>')),

    operator: ($) =>
      token(
        choice(
          '==',
          '!=',
          '~|',
          '~&',
          '~A',
          '~B',
          '->',
          '<-',
          '^',
          '&',
          '|',
          '\\',
          '/',
          '~',
          '=',
          '+',
          '-',
        ),
      ),
  },
});
