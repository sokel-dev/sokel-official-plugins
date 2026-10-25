"""Single-attempt List member mutations using the caller's guarded session."""

import re


# Request IDs, feature flags, and data.list response schema verified against:
# https://github.com/fa0311/twitter-openapi/tree/252713ac47471859282c65ff162eba3658a88c2c
# src/config/placeholder.json and src/openapi/paths/list.yaml.
MEMBER_OPERATIONS = {
    "add_list_member": ("ListAddMember", "-Kf1YVC1rNKkPvPFk7YXZw"),
    "remove_list_member": ("ListRemoveMember", "_2o1-wSMWGFSi5HUscn1Hw"),
}
MEMBER_FEATURES = {
    "profile_label_improvements_pcf_label_in_post_enabled": True,
    "responsive_web_profile_redirect_enabled": True,
    "rweb_tipjar_consumption_enabled": False,
    "verified_phone_label_enabled": False,
    "responsive_web_graphql_timeline_navigation_enabled": True,
}


def change_member(client, operation, list_id, username, error_factory):
    """Resolve a username and submit one mutation, without retries or readback.

    Inputs have already been validated by the plugin boundary. The caller owns
    the guarded session and its deadline, cancellation, proxy, and error handling.
    ``changed`` confirms X accepted the operation, not a measured membership delta.
    """
    if operation not in MEMBER_OPERATIONS:
        raise ValueError("Unsupported List member operation")

    profile = client.user_profile(username)
    if not isinstance(profile, dict) or profile.get("ok") is not True:
        raise error_factory()
    user_id = profile.get("id")
    resolved_username = profile.get("username")
    if (not isinstance(user_id, str) or not re.fullmatch(r"[0-9]{1,20}", user_id)
            or not isinstance(resolved_username, str)
            or not re.fullmatch(r"[A-Za-z0-9_]{1,15}", resolved_username)
            or resolved_username.lower() != username.lower()):
        raise error_factory()

    name, query_id = MEMBER_OPERATIONS[operation]
    path = f"/i/api/graphql/{query_id}/{name}"
    response = client._session.post(
        f"https://x.com{path}",
        json={
            "variables": {"listId": list_id, "userId": user_id},
            "features": MEMBER_FEATURES,
            "queryId": query_id,
        },
        headers=client._headers("POST", path, {"content-type": "application/json"}),
        timeout=client.timeout,
    )
    # GuardedSession has checked the HTTP status and GraphQL errors. Require the
    # mutation's documented List result, not merely an HTTP 200 or arbitrary data.
    body = response.json()
    data = body.get("data") if isinstance(body, dict) else None
    result = data.get("list") if isinstance(data, dict) else None
    if (not isinstance(result, dict) or result.get("id_str") != list_id
            or not isinstance(result.get("name"), str) or not result["name"].strip()
            or type(result.get("member_count")) is not int or result["member_count"] < 0):
        raise error_factory()
    # List.is_member describes the logged-in viewer; it cannot confirm the
    # requested target's membership. X may also accept an already-satisfied state.
    return {
        "list_id": list_id, "user_id": user_id,
        "username": resolved_username, "changed": True,
    }
