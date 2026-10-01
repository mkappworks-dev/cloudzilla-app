package model

// OAuthStatePurposeLink marks a state that connects a provider account to the user who started the flow.
const OAuthStatePurposeLink = "link"

// OAuthStatePurposeReauth marks a state for a fresh sign-in that confirms a sensitive action.
const OAuthStatePurposeReauth = "reauth"
