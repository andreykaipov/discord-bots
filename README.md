# work in progress

## setup

From your personal `github` 1Password Vault, get the service account token and set it in your current environment:

```
export OP_SERVICE_ACCOUNT_TOKEN=...
```

For your new bot, run `./scripts/sync-secrets.sh` to copy the secrets from 1Password to Cloudflare Workers and your local machine as `.dev.vars` for your new bot.

## local dev

Start the bot locally:

```console
❯ (cd minecraft-servers; wrangler dev)
```

You can test the bot locally without deploying to CF workers by opening up an Argo tunnel and pointing the Discord app's interactions endpoint to `https://wrangler.kaipov.com`.
I hope it still works!

```console
❯ ./script/cloudflared-tunnel
```

## adding your new bot to your server

Go to `https://discord.com/developers/applications/$blah/oauth2/url-generator` and select the scopes you want your bot to have.
Then go to that URL in and add it to a server.

### misc

```console
❯ curl https://wrangler.kaipov.com/message/$channelid -d 'message here you want to send'
```
