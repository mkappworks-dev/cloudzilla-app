package components

import "context"

type avatarKeysCtxKey struct{}

// WithAvatarKeys makes keys, a map from user or org name to avatar key,
// available to every Avatar rendered with the returned context. Keys already
// on ctx are kept unless keys replaces them.
func WithAvatarKeys(ctx context.Context, keys map[string]string) context.Context {
	if len(keys) == 0 {
		return ctx
	}
	merged := make(map[string]string, len(keys))
	if prev, ok := ctx.Value(avatarKeysCtxKey{}).(map[string]string); ok {
		for k, v := range prev {
			merged[k] = v
		}
	}
	for k, v := range keys {
		merged[k] = v
	}
	return context.WithValue(ctx, avatarKeysCtxKey{}, merged)
}

func avatarKeyFor(ctx context.Context, name string) string {
	keys, _ := ctx.Value(avatarKeysCtxKey{}).(map[string]string)
	return keys[name]
}

// AvatarURL is where an avatar key is served, or "" for no avatar. Keys start
// with "avatars/", which is also the serving route's prefix.
func AvatarURL(key string) string {
	if key == "" {
		return ""
	}
	return "/" + key
}
