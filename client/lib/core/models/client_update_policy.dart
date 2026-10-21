/// How a solution treats updates of the installed desktop client.
///
/// Stored as `client_update_policy` in File Options, next to the Kiosk Mode
/// keys, because the two policies answer the same question: how much of the
/// platform an end user is allowed to see.
enum ClientUpdatePolicy {
  /// Any signed-in account is offered the update (the default).
  prompt,

  /// Only accounts that can administer the solution are offered it, so an
  /// end user is never interrupted by a download they cannot authorize.
  adminOnly,

  /// Never check. For locked-down deployments that roll clients out with their
  /// own device management.
  off;

  static ClientUpdatePolicy fromJson(String? raw) {
    switch ((raw ?? '').trim().toLowerCase()) {
      case 'admin-only':
      case 'admin_only':
        return ClientUpdatePolicy.adminOnly;
      case 'off':
      case 'disabled':
        return ClientUpdatePolicy.off;
      default:
        return ClientUpdatePolicy.prompt;
    }
  }

  String get jsonValue {
    switch (this) {
      case ClientUpdatePolicy.prompt:
        return 'prompt';
      case ClientUpdatePolicy.adminOnly:
        return 'admin-only';
      case ClientUpdatePolicy.off:
        return 'off';
    }
  }

  String get label {
    switch (this) {
      case ClientUpdatePolicy.prompt:
        return 'Offer the update to every account';
      case ClientUpdatePolicy.adminOnly:
        return 'Offer the update to administrators only';
      case ClientUpdatePolicy.off:
        return 'Never check for client updates';
    }
  }
}
