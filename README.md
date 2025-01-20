# Revezamento Nostr Simples (Nostr Relay Simple - nrs)

## Exemplo de configuração

```yaml
info:
  name: "My Application"
  description: "A description of my application"
  pub_key: "npub1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
  contact: "myemail@example.com"
  url: "https://example.com"
  icon: "https://example.com/icon.png"
blossom:
  enabled: true
  auth_required: false
stream:
  relays:
    - "wss://relay.example.com"
    - "ws://relay2.example.com"
  enabled: true
app_env: "development"
base_path: "."
negentropy: false
auth_required: true
```